package handlers

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"angular-go-starter/backend/internal/auth/middleware"
	"angular-go-starter/backend/internal/auth/token"
	"angular-go-starter/backend/internal/bookings/domain"
)

type fakeBookingService struct {
	page  int
	limit int
}

func (*fakeBookingService) Create(context.Context, string, domain.CreateInput) (domain.Order, error) {
	return domain.Order{}, nil
}

func (*fakeBookingService) Get(context.Context, string, string, string) (domain.Order, error) {
	return domain.Order{}, nil
}

func (s *fakeBookingService) ListAll(_ context.Context, page, limit int) (domain.AdminBookingPage, error) {
	s.page, s.limit = page, limit
	return domain.AdminBookingPage{
		Items: []domain.AdminBooking{{
			Order:      domain.Order{Quantity: 2},
			UserEmail:  "buyer@example.com",
			EventTitle: "Concert",
		}},
		Page:  page,
		Limit: limit,
		Total: 1,
	}, nil
}

func (*fakeBookingService) Confirm(context.Context, string, string) (domain.Order, error) {
	return domain.Order{}, nil
}

func (*fakeBookingService) Cancel(context.Context, string, string) (domain.Order, error) {
	return domain.Order{}, nil
}

func TestAdminListReturnsPaginatedBookingDetails(t *testing.T) {
	t.Parallel()

	bookings := &fakeBookingService{}
	handler := New(bookings)
	response := httptest.NewRecorder()
	handler.AdminList(response, httptest.NewRequest(http.MethodGet, "/api/admin/bookings?page=2&limit=5", nil))

	if response.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", response.Code, response.Body.String())
	}
	if bookings.page != 2 || bookings.limit != 5 {
		t.Fatalf("unexpected pagination: page=%d limit=%d", bookings.page, bookings.limit)
	}
	var result domain.AdminBookingPage
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(result.Items) != 1 || result.Items[0].UserEmail != "buyer@example.com" ||
		result.Items[0].EventTitle != "Concert" || result.Items[0].Quantity != 2 {
		t.Fatalf("unexpected admin bookings response: %+v", result)
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
		authenticator.RequireRoles("admin")(http.HandlerFunc(New(&fakeBookingService{}).AdminList)),
	)
	request := httptest.NewRequest(http.MethodGet, "/api/admin/bookings", nil)
	request.Header.Set("Authorization", "Bearer "+rawToken)
	response := httptest.NewRecorder()
	protected.ServeHTTP(response, request)

	if response.Code != http.StatusForbidden {
		t.Fatalf("expected status 403 for non-admin, got %d", response.Code)
	}
}

type noRevocations struct{}

func (noRevocations) IsRevoked(context.Context, string) (bool, error) {
	return false, nil
}
