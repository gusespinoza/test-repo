package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"angular-go-starter/backend/internal/events/domain"
)

type fakeEventService struct {
	page  int
	limit int
}

func (s *fakeEventService) List(_ context.Context, page, limit int) (domain.EventPage, error) {
	s.page, s.limit = page, limit
	return domain.EventPage{Items: []domain.Event{}, Page: page, Limit: limit, Total: 0}, nil
}

func (*fakeEventService) FindByID(context.Context, string) (domain.Event, error) {
	return domain.Event{}, domain.ErrEventNotFound
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
