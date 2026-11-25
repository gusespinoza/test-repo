//go:build integration

package integration_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/redis/go-redis/v9"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"angular-go-starter/backend/internal/auth/middleware"
	"angular-go-starter/backend/internal/auth/token"
	"angular-go-starter/backend/internal/bookings/domain"
	"angular-go-starter/backend/internal/bookings/handlers"
	bookingrepository "angular-go-starter/backend/internal/bookings/repository"
	bookingservice "angular-go-starter/backend/internal/bookings/service"
)

const (
	bookingsDBName     = "tickets"
	bookingsDBUser     = "tickets"
	bookingsDBPassword = "bookings-test-password"
	bookingAdminID     = "11111111-1111-4111-8111-111111111111"
	bookingUserOneID   = "22222222-2222-4222-8222-222222222222"
	bookingUserTwoID   = "33333333-3333-4333-8333-333333333333"
)

type noRevocations struct{}

func (noRevocations) IsRevoked(context.Context, string) (bool, error) {
	return false, nil
}

func TestBookingReservationConfirmationCancellationAndConcurrency(t *testing.T) {
	db := startBookingsPostgres(t)
	redisClient := startBookingsRedis(t)
	applyBookingSchemas(t, db)
	insertBookingUsers(t, db)

	tokens, err := token.NewManager("booking-integration-secret-0123456789", "booking-integration", 15*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	authenticator := middleware.NewAuthenticator(tokens, noRevocations{}, log.New(io.Discard, "", 0))
	bookingHandler := handlers.New(bookingservice.New(bookingrepository.NewPostgresRepository(db, redisClient)))
	mux := http.NewServeMux()
	mux.Handle("POST /api/bookings", authenticator.RequireAuthentication(http.HandlerFunc(bookingHandler.Create)))
	mux.Handle("GET /api/bookings/{id}", authenticator.RequireAuthentication(http.HandlerFunc(bookingHandler.Get)))
	mux.Handle("POST /api/bookings/{id}/confirm", authenticator.RequireAuthentication(http.HandlerFunc(bookingHandler.Confirm)))
	mux.Handle("DELETE /api/bookings/{id}", authenticator.RequireAuthentication(http.HandlerFunc(bookingHandler.Cancel)))

	adminToken := issueBookingToken(t, tokens, bookingAdminID, "admin")
	userOneToken := issueBookingToken(t, tokens, bookingUserOneID, "requester")
	userTwoToken := issueBookingToken(t, tokens, bookingUserTwoID, "requester")

	t.Run("reservation is idempotent and confirmation is transactional", func(t *testing.T) {
		eventID := insertBookingEvent(t, db, 4)
		body := bookingRequestBody(eventID, 2, "same-request")

		first := sendBookingRequest(t, mux, http.MethodPost, "/api/bookings", body, userOneToken)
		if first.Code != http.StatusCreated {
			t.Fatalf("create booking returned %d: %s", first.Code, first.Body.String())
		}
		order := decodeOrder(t, first)
		if order.Status != domain.StatusPending || order.Quantity != 2 || order.UserID != bookingUserOneID {
			t.Fatalf("unexpected pending order: %+v", order)
		}
		timeUntilExpiry := time.Until(order.ExpiresAt)
		if timeUntilExpiry <= 9*time.Minute || timeUntilExpiry > 10*time.Minute+time.Second {
			t.Fatalf("reservation expiry is not close to 10 minutes from now: %s", order.ExpiresAt)
		}

		retry := sendBookingRequest(t, mux, http.MethodPost, "/api/bookings", body, userOneToken)
		if retry.Code != http.StatusCreated {
			t.Fatalf("idempotent retry returned %d: %s", retry.Code, retry.Body.String())
		}
		retriedOrder := decodeOrder(t, retry)
		if retriedOrder.ID != order.ID {
			t.Fatalf("idempotent retry created order %q, want existing order %q", retriedOrder.ID, order.ID)
		}
		assertOrderCount(t, db, "same-request", bookingUserOneID, 1)
		locked, err := redisClient.Get(context.Background(), "booking:lock:"+eventID).Int()
		if err != nil || locked != 2 {
			t.Fatalf("Redis held quantity = %d, err=%v; want 2", locked, err)
		}
		lockTTL, err := redisClient.TTL(context.Background(), "booking:lock:"+eventID).Result()
		if err != nil || lockTTL <= 0 || lockTTL > 10*time.Minute {
			t.Fatalf("Redis lock TTL = %s, err=%v; want (0, 10m]", lockTTL, err)
		}

		confirm := sendBookingRequest(
			t,
			mux,
			http.MethodPost,
			"/api/bookings/"+order.ID+"/confirm",
			"",
			userOneToken,
		)
		if confirm.Code != http.StatusOK {
			t.Fatalf("confirm returned %d: %s", confirm.Code, confirm.Body.String())
		}
		confirmed := decodeOrder(t, confirm)
		if confirmed.Status != domain.StatusConfirmed {
			t.Fatalf("status = %q, want confirmed", confirmed.Status)
		}
		assertEventSold(t, db, eventID, 2)

		adminGet := sendBookingRequest(t, mux, http.MethodGet, "/api/bookings/"+order.ID, "", adminToken)
		if adminGet.Code != http.StatusOK {
			t.Fatalf("admin GET returned %d: %s", adminGet.Code, adminGet.Body.String())
		}
	})

	t.Run("rejects quantity above availability", func(t *testing.T) {
		eventID := insertBookingEvent(t, db, 1)
		response := sendBookingRequest(
			t,
			mux,
			http.MethodPost,
			"/api/bookings",
			bookingRequestBody(eventID, 2, "too-many"),
			userOneToken,
		)
		if response.Code != http.StatusConflict {
			t.Fatalf("oversized reservation returned %d, want 409: %s", response.Code, response.Body.String())
		}
	})

	t.Run("cancellation releases availability", func(t *testing.T) {
		eventID := insertBookingEvent(t, db, 1)
		first := sendBookingRequest(
			t,
			mux,
			http.MethodPost,
			"/api/bookings",
			bookingRequestBody(eventID, 1, "cancel-me"),
			userOneToken,
		)
		if first.Code != http.StatusCreated {
			t.Fatalf("create booking returned %d: %s", first.Code, first.Body.String())
		}
		order := decodeOrder(t, first)
		cancel := sendBookingRequest(t, mux, http.MethodDelete, "/api/bookings/"+order.ID, "", userOneToken)
		if cancel.Code != http.StatusOK {
			t.Fatalf("cancel returned %d: %s", cancel.Code, cancel.Body.String())
		}
		if cancelled := decodeOrder(t, cancel); cancelled.Status != domain.StatusCancelled {
			t.Fatalf("status = %q, want cancelled", cancelled.Status)
		}

		next := sendBookingRequest(
			t,
			mux,
			http.MethodPost,
			"/api/bookings",
			bookingRequestBody(eventID, 1, "after-cancel"),
			userTwoToken,
		)
		if next.Code != http.StatusCreated {
			t.Fatalf("booking after cancellation returned %d: %s", next.Code, next.Body.String())
		}
		nextOrder := decodeOrder(t, next)
		confirmed := sendBookingRequest(
			t,
			mux,
			http.MethodPost,
			"/api/bookings/"+nextOrder.ID+"/confirm",
			"",
			userTwoToken,
		)
		if confirmed.Code != http.StatusOK {
			t.Fatalf("confirm booking after cancellation returned %d: %s", confirmed.Code, confirmed.Body.String())
		}
		assertEventSold(t, db, eventID, 1)

		cancelConfirmed := sendBookingRequest(
			t,
			mux,
			http.MethodDelete,
			"/api/bookings/"+nextOrder.ID,
			"",
			userTwoToken,
		)
		if cancelConfirmed.Code != http.StatusOK {
			t.Fatalf("cancel confirmed booking returned %d: %s", cancelConfirmed.Code, cancelConfirmed.Body.String())
		}
		assertEventSold(t, db, eventID, 0)
		last := sendBookingRequest(
			t,
			mux,
			http.MethodPost,
			"/api/bookings",
			bookingRequestBody(eventID, 1, "after-confirmed-cancel"),
			userOneToken,
		)
		if last.Code != http.StatusCreated {
			t.Fatalf("booking after confirmed cancellation returned %d: %s", last.Code, last.Body.String())
		}
	})

	t.Run("only one user reserves and confirms the last ticket", func(t *testing.T) {
		eventID := insertBookingEvent(t, db, 1)
		bodyOne := bookingRequestBody(eventID, 1, "last-ticket-user-one")
		bodyTwo := bookingRequestBody(eventID, 1, "last-ticket-user-two")
		type result struct {
			response *httptest.ResponseRecorder
		}
		start := make(chan struct{})
		results := make(chan result, 2)
		var workers sync.WaitGroup
		for _, candidate := range []struct {
			body, accessToken string
		}{
			{body: bodyOne, accessToken: userOneToken},
			{body: bodyTwo, accessToken: userTwoToken},
		} {
			workers.Add(1)
			go func(body, accessToken string) {
				defer workers.Done()
				<-start
				response := sendBookingRequest(t, mux, http.MethodPost, "/api/bookings", body, accessToken)
				results <- result{response: response}
			}(candidate.body, candidate.accessToken)
		}
		close(start)
		workers.Wait()
		close(results)

		var winner *domain.Order
		statuses := map[int]int{}
		for result := range results {
			statuses[result.response.Code]++
			switch result.response.Code {
			case http.StatusCreated:
				order := decodeOrder(t, result.response)
				winner = &order
			case http.StatusConflict:
			default:
				t.Errorf("concurrent reservation returned %d: %s", result.response.Code, result.response.Body.String())
			}
		}
		if statuses[http.StatusCreated] != 1 || statuses[http.StatusConflict] != 1 || winner == nil {
			t.Fatalf("concurrent reservation statuses = %v, want one 201 and one 409", statuses)
		}
		winnerToken := userOneToken
		if winner.UserID == bookingUserTwoID {
			winnerToken = userTwoToken
		}
		confirm := sendBookingRequest(t, mux, http.MethodPost, "/api/bookings/"+winner.ID+"/confirm", "", winnerToken)
		if confirm.Code != http.StatusOK {
			t.Fatalf("winning confirmation returned %d: %s", confirm.Code, confirm.Body.String())
		}
		if confirmed := decodeOrder(t, confirm); confirmed.Status != domain.StatusConfirmed {
			t.Fatalf("winning order status = %q, want confirmed", confirmed.Status)
		}
		assertEventSold(t, db, eventID, 1)
	})
}

func startBookingsPostgres(t *testing.T) *sql.DB {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        "postgres:16-alpine",
			ExposedPorts: []string{"5432/tcp"},
			Env: map[string]string{
				"POSTGRES_DB":       bookingsDBName,
				"POSTGRES_USER":     bookingsDBUser,
				"POSTGRES_PASSWORD": bookingsDBPassword,
			},
			WaitingFor: wait.ForListeningPort("5432/tcp").WithStartupTimeout(90 * time.Second),
		},
		Started: true,
	})
	if err != nil {
		t.Fatalf("start PostgreSQL test container: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := container.Terminate(cleanupCtx); err != nil {
			t.Errorf("terminate PostgreSQL test container: %v", err)
		}
	})

	host, err := container.Host(ctx)
	if err != nil {
		t.Fatalf("get PostgreSQL container host: %v", err)
	}
	port, err := container.MappedPort(ctx, "5432/tcp")
	if err != nil {
		t.Fatalf("get PostgreSQL mapped port: %v", err)
	}
	connectionString := fmt.Sprintf(
		"postgres://%s:%s@%s/%s?sslmode=disable",
		bookingsDBUser,
		bookingsDBPassword,
		net.JoinHostPort(host, port.Port()),
		bookingsDBName,
	)
	db, err := sql.Open("pgx", connectionString)
	if err != nil {
		t.Fatalf("open PostgreSQL connection: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("close PostgreSQL connection: %v", err)
		}
	})
	pingPostgres(t, ctx, db)
	return db
}

func startBookingsRedis(t *testing.T) *redis.Client {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        "redis:8-alpine",
			ExposedPorts: []string{"6379/tcp"},
			WaitingFor:   wait.ForListeningPort("6379/tcp").WithStartupTimeout(90 * time.Second),
		},
		Started: true,
	})
	if err != nil {
		t.Fatalf("start Redis test container: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := container.Terminate(cleanupCtx); err != nil {
			t.Errorf("terminate Redis test container: %v", err)
		}
	})
	host, err := container.Host(ctx)
	if err != nil {
		t.Fatalf("get Redis container host: %v", err)
	}
	port, err := container.MappedPort(ctx, "6379/tcp")
	if err != nil {
		t.Fatalf("get Redis mapped port: %v", err)
	}
	client := redis.NewClient(&redis.Options{Addr: net.JoinHostPort(host, port.Port())})
	t.Cleanup(func() {
		if err := client.Close(); err != nil {
			t.Errorf("close Redis client: %v", err)
		}
	})
	if err := client.Ping(ctx).Err(); err != nil {
		t.Fatalf("ping Redis test container: %v", err)
	}
	return client
}

func pingPostgres(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	pingCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		if err := db.PingContext(pingCtx); err == nil {
			return
		}
		select {
		case <-pingCtx.Done():
			t.Fatalf("ping PostgreSQL test container: %v", pingCtx.Err())
		case <-ticker.C:
		}
	}
}

func applyBookingSchemas(t *testing.T, db *sql.DB) {
	t.Helper()
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve integration test file path")
	}
	base := filepath.Dir(currentFile)
	schemas := []string{
		filepath.Join(base, "..", "..", "auth", "repository", "schema.sql"),
		filepath.Join(base, "..", "..", "events", "repository", "schema.sql"),
		filepath.Join(base, "..", "repository", "schema.sql"),
	}
	for _, path := range schemas {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read schema %s: %v", path, err)
		}
		for _, statement := range strings.Split(string(content), ";") {
			if strings.TrimSpace(statement) == "" {
				continue
			}
			if _, err := db.Exec(statement); err != nil {
				t.Fatalf("apply schema statement from %s: %v", path, err)
			}
		}
	}
}

func insertBookingUsers(t *testing.T, db *sql.DB) {
	t.Helper()
	for _, user := range []struct {
		id, email, role string
	}{
		{bookingAdminID, "booking-admin@example.com", "admin"},
		{bookingUserOneID, "booking-one@example.com", "requester"},
		{bookingUserTwoID, "booking-two@example.com", "requester"},
	} {
		if _, err := db.Exec(
			`INSERT INTO auth_users (id, email, password_hash, role) VALUES ($1::uuid, $2, 'unused', $3)`,
			user.id,
			user.email,
			user.role,
		); err != nil {
			t.Fatalf("insert booking test user: %v", err)
		}
	}
}

func insertBookingEvent(t *testing.T, db *sql.DB, capacity int) string {
	t.Helper()
	var eventID string
	err := db.QueryRow(`
		INSERT INTO events (title, venue, starts_at, capacity)
		VALUES ('Integration event', 'Main Hall', now() + interval '1 day', $1)
		RETURNING id::text
	`, capacity).Scan(&eventID)
	if err != nil {
		t.Fatalf("insert booking test event: %v", err)
	}
	return eventID
}

func issueBookingToken(t *testing.T, tokens *token.Manager, userID, role string) string {
	t.Helper()
	raw, _, err := tokens.Issue(userID, role)
	if err != nil {
		t.Fatalf("issue %s test token: %v", role, err)
	}
	return raw
}

func bookingRequestBody(eventID string, quantity int, key string) string {
	return fmt.Sprintf(
		`{"event_id":%q,"quantity":%d,"idempotency_key":%q}`,
		eventID,
		quantity,
		key,
	)
}

func sendBookingRequest(
	t *testing.T,
	handler http.Handler,
	method, path, body, accessToken string,
) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	if accessToken != "" {
		request.Header.Set("Authorization", "Bearer "+accessToken)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func decodeOrder(t *testing.T, response *httptest.ResponseRecorder) domain.Order {
	t.Helper()
	var order domain.Order
	if err := json.Unmarshal(response.Body.Bytes(), &order); err != nil {
		t.Fatalf("decode booking response: %v", err)
	}
	return order
}

func assertOrderCount(t *testing.T, db *sql.DB, key, userID string, expected int) {
	t.Helper()
	var count int
	if err := db.QueryRow(
		`SELECT count(*) FROM orders WHERE user_id = $1::uuid AND idempotency_key = $2`,
		userID,
		key,
	).Scan(&count); err != nil {
		t.Fatalf("count idempotent orders: %v", err)
	}
	if count != expected {
		t.Fatalf("order count = %d, want %d", count, expected)
	}
}

func assertEventSold(t *testing.T, db *sql.DB, eventID string, expected int) {
	t.Helper()
	var sold int
	if err := db.QueryRow(`SELECT sold FROM events WHERE id = $1::uuid`, eventID).Scan(&sold); err != nil {
		t.Fatalf("query sold tickets: %v", err)
	}
	if sold != expected {
		t.Fatalf("sold = %d, want %d", sold, expected)
	}
}
