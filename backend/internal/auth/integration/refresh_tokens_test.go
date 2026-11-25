//go:build integration

package integration_test

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
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
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
	"golang.org/x/crypto/bcrypt"

	"angular-go-starter/backend/internal/auth/handlers"
	"angular-go-starter/backend/internal/auth/repository"
	"angular-go-starter/backend/internal/auth/service"
	"angular-go-starter/backend/internal/auth/token"
)

const (
	testDBName     = "tickets"
	testDBUser     = "tickets"
	testDBPassword = "testcontainers-password"
	refreshCookie  = "refresh_token"
)

type noOpRevocationStore struct{}

func (noOpRevocationStore) Revoke(context.Context, string, time.Duration) error {
	return nil
}

func TestRefreshTokenRotationReplayAndConcurrency(t *testing.T) {
	db := startPostgres(t)
	applySchema(t, db)
	insertTestUser(t, db)

	tokens, err := token.NewManager("integration-test-secret-0123456789", "integration-test", 15*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	authService, err := service.New(
		repository.NewPostgresUserRepository(db),
		noOpRevocationStore{},
		repository.NewPostgresRefreshTokenRepository(db),
		tokens,
	)
	if err != nil {
		t.Fatal(err)
	}
	authHandler := handlers.New(authService)
	mux := http.NewServeMux()
	mux.Handle("POST /api/auth/login", http.HandlerFunc(authHandler.Login))
	mux.Handle("POST /api/auth/refresh", http.HandlerFunc(authHandler.Refresh))

	t.Run("rotation then replay revokes the family", func(t *testing.T) {
		loginAccessToken, oldRefreshToken := login(t, mux)
		if loginAccessToken == "" {
			t.Fatal("login did not return an access token")
		}
		oldFamilyID, revoked := refreshTokenState(t, db, oldRefreshToken)
		if revoked {
			t.Fatal("newly issued refresh token is already revoked")
		}

		rotation := refresh(t, mux, oldRefreshToken)
		if rotation.Code != http.StatusOK {
			t.Fatalf("refresh returned %d: %s", rotation.Code, rotation.Body.String())
		}
		rotatedAccessToken := accessToken(t, rotation)
		if rotatedAccessToken == "" || rotatedAccessToken == loginAccessToken {
			t.Fatal("refresh response has no new access token")
		}
		newCookie := responseCookie(t, rotation)
		if !newCookie.HttpOnly || !newCookie.Secure || newCookie.SameSite != http.SameSiteLaxMode {
			t.Fatalf("rotated refresh cookie has insecure attributes: %+v", newCookie)
		}
		newRefreshToken := newCookie.Value
		if newRefreshToken == oldRefreshToken {
			t.Fatal("refresh did not rotate the token")
		}
		if _, revoked := refreshTokenState(t, db, oldRefreshToken); !revoked {
			t.Fatal("old refresh token was not marked revoked")
		}
		newFamilyID, newTokenRevoked := refreshTokenState(t, db, newRefreshToken)
		if newFamilyID != oldFamilyID || newTokenRevoked {
			t.Fatal("rotated refresh token was not stored active in the original family")
		}

		replay := refresh(t, mux, oldRefreshToken)
		if replay.Code != http.StatusUnauthorized {
			t.Fatalf("replayed refresh token returned %d, want 401", replay.Code)
		}
		assertFamilyRevoked(t, db, oldFamilyID)
	})

	t.Run("concurrent rotation permits only one request", func(t *testing.T) {
		_, oldRefreshToken := login(t, mux)
		familyID, revoked := refreshTokenState(t, db, oldRefreshToken)
		if revoked {
			t.Fatal("newly issued refresh token is already revoked")
		}

		type result struct {
			status int
			body   string
		}
		results := make(chan result, 2)
		start := make(chan struct{})
		var workers sync.WaitGroup
		for i := 0; i < 2; i++ {
			workers.Add(1)
			go func() {
				defer workers.Done()
				<-start
				recorder := httptest.NewRecorder()
				request := httptest.NewRequest(http.MethodPost, "/api/auth/refresh", nil)
				request.AddCookie(&http.Cookie{Name: refreshCookie, Value: oldRefreshToken})
				mux.ServeHTTP(recorder, request)
				results <- result{status: recorder.Code, body: recorder.Body.String()}
			}()
		}
		close(start)
		workers.Wait()
		close(results)

		statuses := map[int]int{}
		for result := range results {
			statuses[result.status]++
			if result.status != http.StatusOK && result.status != http.StatusUnauthorized {
				t.Errorf("concurrent refresh returned %d: %s", result.status, result.body)
			}
		}
		if statuses[http.StatusOK] != 1 || statuses[http.StatusUnauthorized] != 1 {
			t.Fatalf("concurrent refresh statuses = %v, want one 200 and one 401", statuses)
		}
		assertFamilyRevoked(t, db, familyID)
	})
}

func startPostgres(t *testing.T) *sql.DB {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        "postgres:17-alpine",
			ExposedPorts: []string{"5432/tcp"},
			Env: map[string]string{
				"POSTGRES_DB":       testDBName,
				"POSTGRES_USER":     testDBUser,
				"POSTGRES_PASSWORD": testDBPassword,
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
		testDBUser,
		testDBPassword,
		net.JoinHostPort(host, port.Port()),
		testDBName,
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
	pingTicker := time.NewTicker(250 * time.Millisecond)
	defer pingTicker.Stop()
	for {
		if err := db.PingContext(pingCtx); err == nil {
			break
		} else {
			select {
			case <-pingCtx.Done():
				t.Fatalf("ping PostgreSQL test container: %v", err)
			case <-pingTicker.C:
			}
		}
	}
	return db
}

func applySchema(t *testing.T, db *sql.DB) {
	t.Helper()
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve integration test file path")
	}
	schemaPath := filepath.Join(filepath.Dir(currentFile), "..", "repository", "schema.sql")
	schema, err := os.ReadFile(schemaPath)
	if err != nil {
		t.Fatalf("read auth schema: %v", err)
	}
	for _, statement := range strings.Split(string(schema), ";") {
		if strings.TrimSpace(statement) == "" {
			continue
		}
		if _, err := db.Exec(statement); err != nil {
			t.Fatalf("apply auth schema statement: %v", err)
		}
	}
}

func insertTestUser(t *testing.T, db *sql.DB) {
	t.Helper()
	passwordHash, err := bcrypt.GenerateFromPassword([]byte("integration-password"), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("hash test password: %v", err)
	}
	if _, err := db.Exec(
		`INSERT INTO auth_users (email, password_hash, role) VALUES ($1, $2, $3)`,
		"agent@example.com",
		string(passwordHash),
		"agent",
	); err != nil {
		t.Fatalf("insert integration user: %v", err)
	}
}

func login(t *testing.T, handler http.Handler) (string, string) {
	t.Helper()
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/auth/login",
		strings.NewReader(`{"email":"agent@example.com","password":"integration-password"}`),
	)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("login returned %d: %s", response.Code, response.Body.String())
	}
	access := accessToken(t, response)
	if access == "" {
		t.Fatal("login response has no access token")
	}
	cookie := responseCookie(t, response)
	if !cookie.HttpOnly || !cookie.Secure || cookie.SameSite != http.SameSiteLaxMode {
		t.Fatalf("login refresh cookie has insecure attributes: %+v", cookie)
	}
	return access, cookie.Value
}

func refresh(t *testing.T, handler http.Handler, refreshToken string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, "/api/auth/refresh", nil)
	request.AddCookie(&http.Cookie{Name: refreshCookie, Value: refreshToken})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func accessToken(t *testing.T, response *httptest.ResponseRecorder) string {
	t.Helper()
	var body struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode auth response: %v", err)
	}
	return body.AccessToken
}

func responseCookie(t *testing.T, response *httptest.ResponseRecorder) *http.Cookie {
	t.Helper()
	for _, cookie := range response.Result().Cookies() {
		if cookie.Name == refreshCookie {
			return cookie
		}
	}
	t.Fatal("response has no refresh cookie")
	return nil
}

func refreshTokenState(t *testing.T, db *sql.DB, rawToken string) (string, bool) {
	t.Helper()
	hash := sha256.Sum256([]byte(rawToken))
	var familyID string
	var revokedAt sql.NullTime
	if err := db.QueryRow(
		`SELECT family_id::text, revoked_at FROM auth_refresh_tokens WHERE token_hash = $1`,
		hash[:],
	).Scan(&familyID, &revokedAt); err != nil {
		t.Fatalf("query refresh token state: %v", err)
	}
	return familyID, revokedAt.Valid
}

func assertFamilyRevoked(t *testing.T, db *sql.DB, familyID string) {
	t.Helper()
	var activeTokens int
	if err := db.QueryRow(
		`SELECT count(*) FROM auth_refresh_tokens WHERE family_id = $1 AND revoked_at IS NULL`,
		familyID,
	).Scan(&activeTokens); err != nil {
		t.Fatalf("query active family tokens: %v", err)
	}
	if activeTokens != 0 {
		t.Fatalf("refresh family %s still has %d active token(s)", familyID, activeTokens)
	}
}
