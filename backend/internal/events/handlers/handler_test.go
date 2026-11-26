package handlers

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"angular-go-starter/backend/internal/auth/middleware"
	"angular-go-starter/backend/internal/auth/token"
	"angular-go-starter/backend/internal/events/domain"
)

type fakeEventService struct {
	page       int
	limit      int
	adminPage  int
	adminLimit int
	detail     domain.EventDetail
}

func (s *fakeEventService) List(_ context.Context, page, limit int) (domain.EventPage, error) {
	s.page, s.limit = page, limit
	return domain.EventPage{Items: []domain.Event{}, Page: page, Limit: limit, Total: 0}, nil
}

func (s *fakeEventService) ListAdmin(_ context.Context, page, limit int) (domain.AdminEventPage, error) {
	s.adminPage, s.adminLimit = page, limit
	return domain.AdminEventPage{
		Items: []domain.AdminEvent{{Event: domain.Event{ID: "event-id"}, Sold: 12}},
		Page:  page,
		Limit: limit,
		Total: 1,
	}, nil
}

func (s *fakeEventService) FindByID(context.Context, string) (domain.EventDetail, error) {
	return s.detail, nil
}

func (*fakeEventService) Create(context.Context, domain.EventInput) (domain.Event, error) {
	return domain.Event{}, nil
}

func (*fakeEventService) Update(context.Context, string, domain.EventInput) (domain.Event, error) {
	return domain.Event{}, nil
}

func TestListUsesDefaultPaginationAndReturnsContract(t *testing.T) {
	t.Parallel()

	events := &fakeEventService{}
	handler := New(events)
	response := httptest.NewRecorder()
	handler.List(response, httptest.NewRequest(http.MethodGet, "/api/events", nil))

	if response.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", response.Code)
	}
	if events.page != defaultPage || events.limit != defaultLimit {
		t.Fatalf("unexpected defaults: page=%d limit=%d", events.page, events.limit)
	}
	var result domain.EventPage
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if result.Page != defaultPage || result.Limit != defaultLimit || result.Items == nil {
		t.Fatalf("unexpected paginated response: %+v", result)
	}
}

func TestListRejectsInvalidPagination(t *testing.T) {
	t.Parallel()

	for _, path := range []string{
		"/api/events?page=0",
		"/api/events?limit=101",
		"/api/events?page=abc",
		"/api/events?page=9223372036854775807&limit=100",
	} {
		t.Run(path, func(t *testing.T) {
			handler := New(&fakeEventService{})
			response := httptest.NewRecorder()
			handler.List(response, httptest.NewRequest(http.MethodGet, path, nil))
			if response.Code != http.StatusBadRequest {
				t.Fatalf("expected status 400, got %d", response.Code)
			}
		})
	}
}

func TestGetIncludesAvailable(t *testing.T) {
	t.Parallel()

	const eventID = "00000000-0000-0000-0000-000000000001"
	handler := New(&fakeEventService{
		detail: domain.EventDetail{
			Event:     domain.Event{ID: eventID, Capacity: 20},
			Available: 7,
		},
	})
	request := httptest.NewRequest(http.MethodGet, "/api/events/"+eventID, nil)
	request.SetPathValue("id", eventID)
	response := httptest.NewRecorder()
	handler.Get(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", response.Code, response.Body.String())
	}
	var result struct {
		Available int `json:"available"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if result.Available != 7 {
		t.Fatalf("expected available=7, got %d", result.Available)
	}
}

func TestAdminListReturnsSoldAndUsesPagination(t *testing.T) {
	t.Parallel()

	events := &fakeEventService{}
	handler := New(events)
	response := httptest.NewRecorder()
	handler.AdminList(response, httptest.NewRequest(http.MethodGet, "/api/admin/events?page=2&limit=5", nil))

	if response.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", response.Code, response.Body.String())
	}
	if events.adminPage != 2 || events.adminLimit != 5 {
		t.Fatalf("unexpected pagination: page=%d limit=%d", events.adminPage, events.adminLimit)
	}
	var result domain.AdminEventPage
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(result.Items) != 1 || result.Items[0].Sold != 12 || result.Items[0].ID != "event-id" {
		t.Fatalf("unexpected admin events response: %+v", result)
	}
}

func TestAdminListRejectsNonAdmin(t *testing.T) {
	t.Parallel()

	tokens, err := token.NewManager("0123456789abcdef0123456789abcdef", "test", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	rawToken, _, err := tokens.Issue("user-123", "requester")
	if err != nil {
		t.Fatal(err)
	}
	authenticator := middleware.NewAuthenticator(tokens, noRevocations{}, log.New(io.Discard, "", 0))
	protected := authenticator.RequireAuthentication(
		authenticator.RequireRoles("admin")(http.HandlerFunc(New(&fakeEventService{}).AdminList)),
	)
	request := httptest.NewRequest(http.MethodGet, "/api/admin/events", nil)
	request.Header.Set("Authorization", "Bearer "+rawToken)
	response := httptest.NewRecorder()
	protected.ServeHTTP(response, request)

	if response.Code != http.StatusForbidden {
		t.Fatalf("expected status 403 for non-admin, got %d", response.Code)
	}
}

func TestWriteEndpointsRejectInvalidDatesAndProtectedFields(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		body string
	}{
		{
			name: "unparseable starts_at",
			body: `{"title":"Conference","venue":"Hall","starts_at":"not-a-date","capacity":1}`,
		},
		{
			name: "client supplied id",
			body: `{"id":"00000000-0000-0000-0000-000000000001","title":"Conference","venue":"Hall","starts_at":"2026-12-01T10:00:00Z","capacity":1}`,
		},
		{
			name: "client supplied created_at",
			body: `{"created_at":"2026-01-01T00:00:00Z","title":"Conference","venue":"Hall","starts_at":"2026-12-01T10:00:00Z","capacity":1}`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			for _, method := range []string{http.MethodPost, http.MethodPut} {
				request := httptest.NewRequest(method, "/api/events", strings.NewReader(test.body))
				if method == http.MethodPut {
					request.SetPathValue("id", "00000000-0000-0000-0000-000000000001")
				}
				response := httptest.NewRecorder()
				handler := New(&fakeEventService{})
				if method == http.MethodPost {
					handler.Create(response, request)
				} else {
					handler.Update(response, request)
				}
				if response.Code != http.StatusBadRequest {
					t.Errorf("%s returned %d, want 400", method, response.Code)
				}
			}

		})
	}
}

type noRevocations struct{}

func (noRevocations) IsRevoked(context.Context, string) (bool, error) {
	return false, nil
}
