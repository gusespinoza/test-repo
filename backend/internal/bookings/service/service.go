package service

import (
	"context"
	"math"
	"strings"

	"angular-go-starter/backend/internal/bookings/domain"
)

const MaxPageSize = 100

type Repository interface {
	CreatePending(ctx context.Context, userID string, input domain.CreateInput) (domain.Order, error)
	FindByID(ctx context.Context, id string) (domain.Order, error)
	ListAll(ctx context.Context, offset, limit int) ([]domain.AdminBooking, int64, error)
	Confirm(ctx context.Context, userID, id string) (domain.Order, error)
	Cancel(ctx context.Context, userID, id string) (domain.Order, error)
}

type Service struct {
	repository Repository
}

func New(repository Repository) *Service {
	return &Service{repository: repository}
}

func (s *Service) Create(ctx context.Context, userID string, input domain.CreateInput) (domain.Order, error) {
	input.EventID = strings.TrimSpace(input.EventID)
	input.EventID = strings.ToLower(input.EventID)
	input.IdempotencyKey = strings.TrimSpace(input.IdempotencyKey)
	if userID == "" || input.EventID == "" || input.IdempotencyKey == "" ||
		len(input.IdempotencyKey) > 200 || input.Quantity <= 0 || int64(input.Quantity) > math.MaxInt32 {
		return domain.Order{}, domain.ErrInvalidBooking
	}
	return s.repository.CreatePending(ctx, userID, input)
}

func (s *Service) Get(ctx context.Context, userID, role, id string) (domain.Order, error) {
	order, err := s.repository.FindByID(ctx, id)
	if err != nil {
		return domain.Order{}, err
	}
	if order.UserID != userID && role != "admin" {
		return domain.Order{}, domain.ErrForbidden
	}
	return order, nil
}

func (s *Service) ListAll(ctx context.Context, page, limit int) (domain.AdminBookingPage, error) {
	if page < 1 || limit < 1 || limit > MaxPageSize {
		return domain.AdminBookingPage{}, domain.ErrInvalidPageRange
	}
	maxInt := int(^uint(0) >> 1)
	if page-1 > maxInt/limit {
		return domain.AdminBookingPage{}, domain.ErrInvalidPageRange
	}

	items, total, err := s.repository.ListAll(ctx, (page-1)*limit, limit)
	if err != nil {
		return domain.AdminBookingPage{}, err
	}
	return domain.AdminBookingPage{Items: items, Page: page, Limit: limit, Total: total}, nil
}

func (s *Service) Confirm(ctx context.Context, userID, id string) (domain.Order, error) {
	if userID == "" {
		return domain.Order{}, domain.ErrForbidden
	}
	order, err := s.repository.Confirm(ctx, userID, id)
	return order, err
}

func (s *Service) Cancel(ctx context.Context, userID, id string) (domain.Order, error) {
	if userID == "" {
		return domain.Order{}, domain.ErrForbidden
	}
	return s.repository.Cancel(ctx, userID, id)
}
