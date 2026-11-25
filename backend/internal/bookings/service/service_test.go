package service

import (
	"context"
	"errors"
	"testing"

	"angular-go-starter/backend/internal/bookings/domain"
)

type fakeRepository struct {
	created bool
	order   domain.Order
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
