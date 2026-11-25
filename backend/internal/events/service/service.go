package service

import (
	"context"
	"strings"

	"angular-go-starter/backend/internal/events/domain"
)

const MaxPageSize = 100

type Repository interface {
	List(ctx context.Context, offset, limit int) ([]domain.Event, int64, error)
	FindByID(ctx context.Context, id string) (domain.Event, error)
	Create(ctx context.Context, input domain.EventInput) (domain.Event, error)
	Update(ctx context.Context, id string, input domain.EventInput) (domain.Event, error)
}

type Service struct {
	repository Repository
}

func New(repository Repository) *Service {
	return &Service{repository: repository}
}

func (s *Service) List(ctx context.Context, page, limit int) (domain.EventPage, error) {
	if page < 1 || limit < 1 || limit > MaxPageSize {
		return domain.EventPage{}, domain.ErrInvalidPageRange
	}
	maxInt := int(^uint(0) >> 1)
	if page-1 > maxInt/limit {
		return domain.EventPage{}, domain.ErrInvalidPageRange
	}

	items, total, err := s.repository.List(ctx, (page-1)*limit, limit)
	if err != nil {
		return domain.EventPage{}, err
	}
	return domain.EventPage{Items: items, Page: page, Limit: limit, Total: total}, nil
}

func (s *Service) FindByID(ctx context.Context, id string) (domain.Event, error) {
	return s.repository.FindByID(ctx, id)
}

func (s *Service) Create(ctx context.Context, input domain.EventInput) (domain.Event, error) {
	input, err := validate(input)
	if err != nil {
		return domain.Event{}, err
	}
	return s.repository.Create(ctx, input)
}

func (s *Service) Update(ctx context.Context, id string, input domain.EventInput) (domain.Event, error) {
	input, err := validate(input)
	if err != nil {
		return domain.Event{}, err
	}
	return s.repository.Update(ctx, id, input)
}

func validate(input domain.EventInput) (domain.EventInput, error) {
	input.Title = strings.TrimSpace(input.Title)
	input.Venue = strings.TrimSpace(input.Venue)
	if input.Title == "" || input.Venue == "" || input.StartsAt.IsZero() || input.Capacity <= 0 {
		return domain.EventInput{}, domain.ErrInvalidEvent
	}
	return input, nil
}
