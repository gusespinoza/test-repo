package handlers

import (
	"context"
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"angular-go-starter/backend/internal/auth/domain"
	"angular-go-starter/backend/internal/auth/middleware"
	"angular-go-starter/backend/internal/auth/service"
	"angular-go-starter/backend/internal/auth/token"
)

type fakeAuthService struct {
	loginResponse   service.LoginResponse
	loginErr        error
	refreshResponse service.LoginResponse
	refreshErr      error
	logoutErr       error
	refreshToken    string
}

func (s *fakeAuthService) Login(context.Context, string, string) (service.LoginResponse, error) {
	return s.loginResponse, s.loginErr
}

func (s *fakeAuthService) Refresh(_ context.Context, refreshToken string) (service.LoginResponse, error) {
	s.refreshToken = refreshToken
	return s.refreshResponse, s.refreshErr
}

func (s *fakeAuthService) Logout(_ context.Context, _ *token.Claims, refreshToken string) error {
	s.refreshToken = refreshToken
	return s.logoutErr
}

type allowAccessToken struct{}

func (allowAccessToken) IsRevoked(context.Context, string) (bool, error) {
	return false, nil
}

func TestLoginSetsSecureHttpOnlyRefreshCookieAndOmitsItFromJSON(t *testing.T) {
	t.Parallel()

	auth := &fakeAuthService{loginResponse: service.LoginResponse{
		AccessToken:      "access-token",
		TokenType:        "Bearer",
		ExpiresIn:        900,
		Role:             "agent",
		RefreshToken:     "opaque-refresh-token",
		RefreshExpiresAt: time.Now().Add(7 * 24 * time.Hour),
	}}
	handler := New(auth)
	request := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(`{"email":"agent@example.com","password":"secret"}`))
	response := httptest.NewRecorder()
	handler.Login(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", response.Code)
	}
	cookie := response.Result().Cookies()[0]
	if cookie.Name != refreshCookieName || cookie.Value != "opaque-refresh-token" {
		t.Fatalf("unexpected refresh cookie: %+v", cookie)
	}
	if !cookie.HttpOnly || !cookie.Secure || cookie.SameSite != http.SameSiteLaxMode || cookie.Path != "/api/auth" {
		t.Fatalf("refresh cookie has incorrect security attributes: %+v", cookie)
	}
	if strings.Contains(response.Body.String(), "opaque-refresh-token") || strings.Contains(response.Body.String(), "RefreshToken") {
		t.Fatal("refresh token leaked into the JSON response")
	}
}

func TestRefreshReplayReturns401AndClearsCookie(t *testing.T) {
	t.Parallel()

	auth := &fakeAuthService{refreshErr: domain.ErrRefreshTokenReplay}
	handler := New(auth)
	request := httptest.NewRequest(http.MethodPost, "/api/auth/refresh", nil)
	request.AddCookie(&http.Cookie{Name: refreshCookieName, Value: "replayed-token"})
	response := httptest.NewRecorder()
	handler.Refresh(response, request)

	if response.Code != http.StatusUnauthorized {
		t.Fatalf("expected status 401, got %d", response.Code)
	}
	cookie := response.Result().Cookies()[0]
	if cookie.Name != refreshCookieName || cookie.MaxAge != -1 {
		t.Fatalf("refresh cookie was not cleared: %+v", cookie)
	}
}

func TestRefreshRotatesCookieAndReturnsAccessToken(t *testing.T) {
	t.Parallel()

	auth := &fakeAuthService{refreshResponse: service.LoginResponse{
		AccessToken:      "new-access-token",
		TokenType:        "Bearer",
		ExpiresIn:        900,
		Role:             "agent",
		RefreshToken:     "new-refresh-token",
		RefreshExpiresAt: time.Now().Add(7 * 24 * time.Hour),
	}}
	handler := New(auth)
	request := httptest.NewRequest(http.MethodPost, "/api/auth/refresh", nil)
	request.AddCookie(&http.Cookie{Name: refreshCookieName, Value: "old-refresh-token"})
	response := httptest.NewRecorder()
	handler.Refresh(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", response.Code)
	}
	if auth.refreshToken != "old-refresh-token" {
		t.Fatalf("refresh token cookie was not passed to service: %q", auth.refreshToken)
	}
	cookie := response.Result().Cookies()[0]
	if cookie.Value != "new-refresh-token" || cookie.MaxAge <= 0 {
		t.Fatalf("new refresh cookie was not set: %+v", cookie)
	}
	if !strings.Contains(response.Body.String(), "new-access-token") {
		t.Fatal("response does not contain the new access token")
	}
	if strings.Contains(response.Body.String(), "new-refresh-token") {
		t.Fatal("refresh token leaked into the JSON response")
	}
}

func TestLogoutRevokesFamilyAndClearsCookie(t *testing.T) {
	t.Parallel()

	tokens, err := token.NewManager("0123456789abcdef0123456789abcdef", "test", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	accessToken, _, err := tokens.Issue("user-123", "agent")
	if err != nil {
		t.Fatal(err)
	}
	auth := &fakeAuthService{}
	handler := New(auth)
	protectedLogout := middleware.NewAuthenticator(tokens, allowAccessToken{}, log.New(io.Discard, "", 0)).
		RequireAuthentication(http.HandlerFunc(handler.Logout))

	request := httptest.NewRequest(http.MethodPost, "/api/auth/logout", nil)
	request.Header.Set("Authorization", "Bearer "+accessToken)
	request.AddCookie(&http.Cookie{Name: refreshCookieName, Value: "current-refresh-token"})
	response := httptest.NewRecorder()
	protectedLogout.ServeHTTP(response, request)

	if response.Code != http.StatusNoContent {
		t.Fatalf("expected status 204, got %d", response.Code)
	}
	if auth.refreshToken != "current-refresh-token" {
		t.Fatalf("logout did not receive refresh cookie: %q", auth.refreshToken)
	}
	cookie := response.Result().Cookies()[0]
	if cookie.Name != refreshCookieName || cookie.MaxAge != -1 {
		t.Fatalf("refresh cookie was not cleared: %+v", cookie)
	}
}

func TestRefreshInternalErrorDoesNotPretendSuccess(t *testing.T) {
	t.Parallel()

	auth := &fakeAuthService{refreshErr: errors.New("database unavailable")}
	handler := New(auth)
	request := httptest.NewRequest(http.MethodPost, "/api/auth/refresh", nil)
	request.AddCookie(&http.Cookie{Name: refreshCookieName, Value: "valid-looking-token"})
	response := httptest.NewRecorder()
	handler.Refresh(response, request)

	if response.Code != http.StatusInternalServerError {
		t.Fatalf("expected status 500, got %d", response.Code)
	}
}
