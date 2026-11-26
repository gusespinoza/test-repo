package service

import (
	"context"
	"errors"
	"testing"

	"angular-go-starter/backend/internal/bookings/domain"
)

type fakeRepository struct {
	created    bool
	order      domain.Order
	listOffset int
	listLimit  int
}

func (r *fakeRepository) CreatePending(_ context.Context, userID string, input domain.CreateInput) (domain.Order, error) {
	r.created = true
	r.order = domain.Order{
		UserID:         userID,
		EventID:        input.EventID,
		Quantity:       input.Quantity,
		IdempotencyKey: input.IdempotencyKey,
	}
	return r.order, nil
}

func (r *fakeRepository) FindByID(context.Context, string) (domain.Order, error) {
	return r.order, nil
}

func (r *fakeRepository) ListAll(_ context.Context, offset, limit int) ([]domain.AdminBooking, int64, error) {
	r.listOffset, r.listLimit = offset, limit
	return []domain.AdminBooking{{UserEmail: "buyer@example.com", EventTitle: "Concert"}}, 31, nil
}

func (*fakeRepository) Confirm(context.Context, string, string) (domain.Order, error) {
	return domain.Order{}, nil
}

func (*fakeRepository) Cancel(context.Context, string, string) (domain.Order, error) {
	return domain.Order{}, nil
}

func TestCreateValidatesAndNormalizesInput(t *testing.T) {
	t.Parallel()

	repository := &fakeRepository{}
	service := New(repository)
	_, err := service.Create(context.Background(), "user-id", domain.CreateInput{
		EventID:        "  ABCDEF00-0000-0000-0000-000000000001 ",
		Quantity:       2,
		IdempotencyKey: "  request-1 ",
	})
	if err != nil {
		t.Fatalf("create booking: %v", err)
	}
	if !repository.created {
		t.Fatal("repository was not called")
	}
	if repository.order.EventID != "abcdef00-0000-0000-0000-000000000001" ||
		repository.order.IdempotencyKey != "request-1" {
		t.Fatalf("input not normalized: %+v", repository.order)
	}
}

func TestCreateRejectsInvalidInput(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		userID string
		input  domain.CreateInput
	}{
		"missing user": {
			input: domain.CreateInput{EventID: "event", Quantity: 1, IdempotencyKey: "key"},
		},
		"missing event": {
			userID: "user", input: domain.CreateInput{Quantity: 1, IdempotencyKey: "key"},
		},
		"zero quantity": {
			userID: "user", input: domain.CreateInput{EventID: "event", IdempotencyKey: "key"},
		},
		"negative quantity": {
			userID: "user", input: domain.CreateInput{EventID: "event", Quantity: -1, IdempotencyKey: "key"},
		},
		"empty idempotency key": {
			userID: "user", input: domain.CreateInput{EventID: "event", Quantity: 1, IdempotencyKey: " "},
		},
		"oversized idempotency key": {
			userID: "user", input: domain.CreateInput{
				EventID: "event", Quantity: 1, IdempotencyKey: string(make([]byte, 201)),
			},
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			repository := &fakeRepository{}
			_, err := New(repository).Create(context.Background(), test.userID, test.input)
			if !errors.Is(err, domain.ErrInvalidBooking) {
				t.Fatalf("expected invalid booking error, got %v", err)
			}
			if repository.created {
				t.Fatal("repository called with invalid input")
			}
		})
	}
}

func TestGetAllowsOwnerOrAdmin(t *testing.T) {
	t.Parallel()

	repository := &fakeRepository{order: domain.Order{UserID: "owner"}}
	service := New(repository)
	if _, err := service.Get(context.Background(), "owner", "requester", "id"); err != nil {
		t.Fatalf("owner should be allowed: %v", err)
	}
	if _, err := service.Get(context.Background(), "admin", "admin", "id"); err != nil {
		t.Fatalf("admin should be allowed: %v", err)
	}
	if _, err := service.Get(context.Background(), "other", "requester", "id"); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("expected forbidden error, got %v", err)
	}
}

func TestListAllCalculatesOffsetAndPageMetadata(t *testing.T) {
	t.Parallel()

	repository := &fakeRepository{}
	result, err := New(repository).ListAll(context.Background(), 3, 7)
	if err != nil {
		t.Fatalf("list all bookings: %v", err)
	}
	if repository.listOffset != 14 || repository.listLimit != 7 {
		t.Fatalf("unexpected query range: offset=%d limit=%d", repository.listOffset, repository.listLimit)
	}
	if result.Page != 3 || result.Limit != 7 || result.Total != 31 ||
		result.Items[0].UserEmail != "buyer@example.com" || result.Items[0].EventTitle != "Concert" {
		t.Fatalf("unexpected admin page result: %+v", result)
	}
}

func TestListAllRejectsInvalidPagination(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		page, limit int
	}{
		{page: 0, limit: 20},
		{page: 1, limit: 101},
		{page: int(^uint(0) >> 1), limit: 100},
	} {
		_, err := New(&fakeRepository{}).ListAll(context.Background(), test.page, test.limit)
		if !errors.Is(err, domain.ErrInvalidPageRange) {
			t.Errorf("ListAll(%d, %d) error = %v, want invalid page range", test.page, test.limit, err)
		}
	}
}
