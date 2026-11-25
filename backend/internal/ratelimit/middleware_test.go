package ratelimit

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type fakeRateCounter struct {
	retryAfter time.Duration
	err        error
	ip         string
	email      string
}

func (c *fakeRateCounter) Check(_ context.Context, ip, email string) (time.Duration, error) {
	c.ip = ip
	c.email = email
	return c.retryAfter, c.err
}

func TestWrapExtractsEmailAndRestoresRequestBody(t *testing.T) {
	t.Parallel()

	counter := &fakeRateCounter{}
	middleware := Middleware{counter: counter, logger: log.New(io.Discard, "", 0)}
	body := `{"email":"  Agent@Example.com ","password":"secret"}`
	handler := middleware.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read downstream request body: %v", err)
			return
		}
		if string(received) != body {
			t.Errorf("request body changed: got %q", received)
		}
		w.WriteHeader(http.StatusNoContent)
	}))

	request := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(body))
	request.RemoteAddr = "[2001:db8::1]:12345"
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusNoContent {
		t.Fatalf("expected request to pass, got status %d", response.Code)
	}
	if counter.ip != "2001:db8::1" {
		t.Errorf("unexpected client IP: %q", counter.ip)
	}
	if counter.email != "agent@example.com" {
		t.Errorf("email was not normalized: %q", counter.email)
	}
}

func TestWrapReturns429AndRetryAfter(t *testing.T) {
	t.Parallel()

	counter := &fakeRateCounter{retryAfter: 23 * time.Second}
	middleware := Middleware{counter: counter, logger: log.New(io.Discard, "", 0)}
	handlerCalled := false
	handler := middleware.Wrap(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		handlerCalled = true
	}))

	request := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewBufferString(`{"email":"a@example.com"}`))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusTooManyRequests {
		t.Fatalf("expected status 429, got %d", response.Code)
	}
	if response.Header().Get("Retry-After") != "23" {
		t.Errorf("unexpected Retry-After header: %q", response.Header().Get("Retry-After"))
	}
	if handlerCalled {
		t.Error("login handler was called after rate limit was exceeded")
	}
}

func TestWrapFailsOpenWhenRedisReturnsError(t *testing.T) {
	t.Parallel()

	var logs bytes.Buffer
	counter := &fakeRateCounter{err: errors.New("Redis unavailable")}
	middleware := Middleware{counter: counter, logger: log.New(&logs, "", 0)}
	handlerCalled := false
	handler := middleware.Wrap(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		handlerCalled = true
		w.WriteHeader(http.StatusNoContent)
	}))

	request := httptest.NewRequest(http.MethodPost, "/api/auth/login", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if !handlerCalled || response.Code != http.StatusNoContent {
		t.Fatalf("request should pass when Redis fails; handlerCalled=%v status=%d", handlerCalled, response.Code)
	}
	if !strings.Contains(logs.String(), "allowing request") {
		t.Errorf("Redis failure was not logged: %q", logs.String())
	}
}
