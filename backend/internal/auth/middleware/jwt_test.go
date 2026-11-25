package middleware

import (
	"context"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"angular-go-starter/backend/internal/auth/token"
)

type fakeRevocationChecker struct {
	revoked bool
}

func (c *fakeRevocationChecker) IsRevoked(context.Context, string) (bool, error) {
	return c.revoked, nil
}

func TestAuthenticationChecksJWTRevocationAndRole(t *testing.T) {
	t.Parallel()

	tokens, err := token.NewManager("0123456789abcdef0123456789abcdef", "test", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	rawToken, _, err := tokens.Issue("user-123", "agent")
	if err != nil {
		t.Fatal(err)
	}

	revocations := &fakeRevocationChecker{}
	auth := NewAuthenticator(tokens, revocations, log.New(io.Discard, "", 0))
	protected := auth.RequireAuthentication(
		auth.RequireRoles("agent")(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		})),
	)

	request := httptest.NewRequest(http.MethodGet, "/api/private", nil)
	request.Header.Set("Authorization", "Bearer "+rawToken)
	response := httptest.NewRecorder()
	protected.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent {
		t.Fatalf("expected authenticated agent to pass, got status %d", response.Code)
	}

	revocations.revoked = true
	response = httptest.NewRecorder()
	protected.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("expected revoked token to be rejected, got status %d", response.Code)
	}
}

func TestRequireRolesRejectsDifferentRole(t *testing.T) {
	t.Parallel()

	tokens, err := token.NewManager("0123456789abcdef0123456789abcdef", "test", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	rawToken, _, err := tokens.Issue("user-123", "requester")
	if err != nil {
		t.Fatal(err)
	}

	auth := NewAuthenticator(tokens, &fakeRevocationChecker{}, log.New(io.Discard, "", 0))
	protected := auth.RequireAuthentication(
		auth.RequireRoles("admin")(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		})),
	)
	request := httptest.NewRequest(http.MethodGet, "/api/admin", nil)
	request.Header.Set("Authorization", "Bearer "+rawToken)
	response := httptest.NewRecorder()
	protected.ServeHTTP(response, request)

	if response.Code != http.StatusForbidden {
		t.Fatalf("expected role mismatch to be rejected, got status %d", response.Code)
	}
}
