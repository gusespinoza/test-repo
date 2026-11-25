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
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	authmiddleware "angular-go-starter/backend/internal/auth/middleware"
	"angular-go-starter/backend/internal/auth/token"
	"angular-go-starter/backend/internal/events/domain"
	"angular-go-starter/backend/internal/events/handlers"
	"angular-go-starter/backend/internal/events/repository"
	"angular-go-starter/backend/internal/events/service"
)

const (
	eventsDBName     = "events"
	eventsDBUser     = "events"
	eventsDBPassword = "events-test-password"
)

type noRevocations struct{}

func (noRevocations) IsRevoked(context.Context, string) (bool, error) {
	return false, nil
}

func TestCreateAuthorizationAndPaginatedList(t *testing.T) {
	db := startEventsPostgres(t)
	applyEventsSchema(t, db)
	assertEventListIndex(t, db)

	tokens, err := token.NewManager(
		"events-integration-secret-0123456789",
		"events-integration",
		15*time.Minute,
	)
	if err != nil {
		t.Fatal(err)
	}
	eventsHandler := handlers.New(service.New(repository.NewPostgresRepository(db)))
	auth := authmiddleware.NewAuthenticator(
		tokens,
		noRevocations{},
		log.New(io.Discard, "", 0),
	)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/events", eventsHandler.List)
	mux.Handle(
		"POST /api/events",
		auth.RequireAuthentication(
			auth.RequireRoles("admin")(http.HandlerFunc(eventsHandler.Create)),
		),
	)

	adminToken := issueToken(t, tokens, "admin-user", "admin")
	nonAdminToken := issueToken(t, tokens, "agent-user", "agent")
	eventBody := `{"title":"Integration conference","description":"Test event","venue":"Main Hall","starts_at":"2026-12-01T10:00:00Z","capacity":120}`

	createResponse := sendEventRequest(
		t,
		mux,
		http.MethodPost,
		"/api/events",
		eventBody,
		adminToken,
	)
	if createResponse.Code != http.StatusCreated {
		t.Fatalf("admin POST returned %d: %s", createResponse.Code, createResponse.Body.String())
	}
	var created domain.Event
	if err := json.Unmarshal(createResponse.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode created event: %v", err)
	}
	if created.ID == "" || created.Title != "Integration conference" || created.Capacity != 120 {
		t.Fatalf("unexpected created event: %+v", created)
	}

	forbiddenResponse := sendEventRequest(
		t,
		mux,
		http.MethodPost,
		"/api/events",
		eventBody,
		nonAdminToken,
	)
	if forbiddenResponse.Code != http.StatusForbidden {
		t.Fatalf("non-admin POST returned %d, want 403: %s", forbiddenResponse.Code, forbiddenResponse.Body.String())
	}

	listResponse := sendEventRequest(t, mux, http.MethodGet, "/api/events?page=1&limit=20", "", "")
	if listResponse.Code != http.StatusOK {
		t.Fatalf("GET list returned %d: %s", listResponse.Code, listResponse.Body.String())
	}
	var page domain.EventPage
	if err := json.Unmarshal(listResponse.Body.Bytes(), &page); err != nil {
		t.Fatalf("decode event page: %v", err)
	}
	if page.Page != 1 || page.Limit != 20 || page.Total != 1 || len(page.Items) != 1 {
		t.Fatalf("unexpected paginated result: %+v", page)
	}
	if page.Items[0].ID != created.ID {
		t.Fatalf("listed event ID %q, want created event %q", page.Items[0].ID, created.ID)
	}
}

func startEventsPostgres(t *testing.T) *sql.DB {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        "postgres:16-alpine",
			ExposedPorts: []string{"5432/tcp"},
			Env: map[string]string{
				"POSTGRES_DB":       eventsDBName,
				"POSTGRES_USER":     eventsDBUser,
				"POSTGRES_PASSWORD": eventsDBPassword,
			},
			WaitingFor: wait.ForListeningPort("5432/tcp").WithStartupTimeout(90 * time.Second),
		},
		Started: true,
	})
	if err != nil {
		t.Fatalf("start PostgreSQL test container: %v", err)
	}
	t.Cleanup(func() {
		terminateCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := container.Terminate(terminateCtx); err != nil {
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
		eventsDBUser,
		eventsDBPassword,
		net.JoinHostPort(host, port.Port()),
		eventsDBName,
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

	pingCtx, cancelPing := context.WithTimeout(ctx, 60*time.Second)
	defer cancelPing()
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		if err := db.PingContext(pingCtx); err == nil {
			break
		} else {
			select {
			case <-pingCtx.Done():
				t.Fatalf("ping PostgreSQL test container: %v", err)
			case <-ticker.C:
			}
		}
	}
	return db
}

func applyEventsSchema(t *testing.T, db *sql.DB) {
	t.Helper()

	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve integration test file path")
	}
	schemaPath := filepath.Join(filepath.Dir(currentFile), "..", "repository", "schema.sql")
	schema, err := os.ReadFile(schemaPath)
	if err != nil {
		t.Fatalf("read events schema: %v", err)
	}
	for _, statement := range strings.Split(string(schema), ";") {
		if strings.TrimSpace(statement) == "" {
			continue
		}
		if _, err := db.Exec(statement); err != nil {
			t.Fatalf("apply events schema statement: %v", err)
		}
	}
}

func assertEventListIndex(t *testing.T, db *sql.DB) {
	t.Helper()

	var definition string
	if err := db.QueryRow(`
		SELECT indexdef
		FROM pg_indexes
		WHERE schemaname = current_schema()
		  AND tablename = 'events'
		  AND indexname = 'events_starts_at_id_idx'
	`).Scan(&definition); err != nil {
		t.Fatalf("query events list index: %v", err)
	}
	if !strings.Contains(definition, "(starts_at, id)") {
		t.Fatalf("events list index does not match ORDER BY columns: %s", definition)
	}
}

func issueToken(t *testing.T, tokens *token.Manager, subject, role string) string {
	t.Helper()
	accessToken, _, err := tokens.Issue(subject, role)
	if err != nil {
		t.Fatalf("issue %s token: %v", role, err)
	}
	return accessToken
}

func sendEventRequest(
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
