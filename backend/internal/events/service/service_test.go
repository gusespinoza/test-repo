package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"angular-go-starter/backend/internal/events/domain"
)

type fakeRepository struct {
	offset     int
	limit      int
	input      domain.EventInput
	createCall bool
	updateCall bool
}

func (r *fakeRepository) List(_ context.Context, offset, limit int) ([]domain.Event, int64, error) {
	r.offset, r.limit = offset, limit
	return []domain.Event{}, 31, nil
}

func (*fakeRepository) FindByID(context.Context, string) (domain.Event, error) {
	return domain.Event{}, domain.ErrEventNotFound
}

func (r *fakeRepository) Create(_ context.Context, input domain.EventInput) (domain.Event, error) {
	r.input = input
	r.createCall = true
	return domain.Event{}, nil
}

func (r *fakeRepository) Update(_ context.Context, _ string, input domain.EventInput) (domain.Event, error) {
	r.input = input
	r.updateCall = true
	return domain.Event{}, domain.ErrEventNotFound
}

func TestListCalculatesOffsetAndPageMetadata(t *testing.T) {
	t.Parallel()

	repository := &fakeRepository{}
	result, err := New(repository).List(context.Background(), 3, 7)
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	if repository.offset != 14 || repository.limit != 7 {
		t.Fatalf("unexpected query range: offset=%d limit=%d", repository.offset, repository.limit)
	}
	if result.Page != 3 || result.Limit != 7 || result.Total != 31 || result.Items == nil {
		t.Fatalf("unexpected page result: %+v", result)
	}
}

func TestCreateValidatesAndNormalizesInput(t *testing.T) {
	t.Parallel()

	repository := &fakeRepository{}
	service := New(repository)
	_, err := service.Create(context.Background(), domain.EventInput{Title: " ", Venue: "Hall"})
	if !errors.Is(err, domain.ErrInvalidEvent) {
		t.Fatalf("expected invalid event error, got %v", err)
	}
	if repository.createCall {
		t.Fatal("repository called with invalid input")
	}

	input := domain.EventInput{
		Title:    "  Conference  ",
		Venue:    "  Main Hall ",
		StartsAt: time.Date(2026, 12, 1, 10, 0, 0, 0, time.UTC),
		Capacity: 50,
	}
	if _, err := service.Create(context.Background(), input); err != nil {
		t.Fatalf("create event: %v", err)
	}
	if repository.input.Title != "Conference" || repository.input.Venue != "Main Hall" {
		t.Fatalf("input was not normalized: %+v", repository.input)
	}
}

func TestCreateAndUpdateRequireValidEventFields(t *testing.T) {
	t.Parallel()

	invalidInputs := map[string]domain.EventInput{
		"blank title": {
			Title: " ", Venue: "Hall", StartsAt: time.Now(), Capacity: 1,
		},
		"blank venue": {
			Title: "Conference", Venue: "  ", StartsAt: time.Now(), Capacity: 1,
		},
		"missing start time": {
			Title: "Conference", Venue: "Hall", Capacity: 1,
		},
		"zero capacity": {
			Title: "Conference", Venue: "Hall", StartsAt: time.Now(), Capacity: 0,
		},
		"negative capacity": {
			Title: "Conference", Venue: "Hall", StartsAt: time.Now(), Capacity: -1,
		},
	}

	for name, input := range invalidInputs {
		t.Run(name, func(t *testing.T) {
			for _, operation := range []string{"create", "update"} {
				repository := &fakeRepository{}
				service := New(repository)
				var err error
				if operation == "create" {
					_, err = service.Create(context.Background(), input)
				} else {
					_, err = service.Update(context.Background(), "event-id", input)
				}
				if !errors.Is(err, domain.ErrInvalidEvent) {
					t.Errorf("%s: expected invalid event error, got %v", operation, err)
				}
				if repository.createCall || repository.updateCall {
					t.Errorf("%s: repository called with invalid input", operation)
				}
			}
		})
	}
}
