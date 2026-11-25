package repository

import (
	"context"
	"database/sql"
	"errors"

	"angular-go-starter/backend/internal/events/domain"
)

type PostgresRepository struct {
	db *sql.DB
}

func NewPostgresRepository(db *sql.DB) *PostgresRepository {
	return &PostgresRepository{db: db}
}

func (r *PostgresRepository) List(ctx context.Context, offset, limit int) ([]domain.Event, int64, error) {
	var total int64
	if err := r.db.QueryRowContext(ctx, `SELECT count(*) FROM events`).Scan(&total); err != nil {
		return nil, 0, err
	}

	rows, err := r.db.QueryContext(ctx, `
		SELECT id::text, title, description, venue, starts_at, capacity, created_at, updated_at
		FROM events
		ORDER BY starts_at ASC, id ASC
		LIMIT $1 OFFSET $2
	`, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	events := make([]domain.Event, 0, limit)
	for rows.Next() {
		event, err := scanEvent(rows)
		if err != nil {
			return nil, 0, err
		}
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	return events, total, nil
}

func (r *PostgresRepository) FindByID(ctx context.Context, id string) (domain.Event, error) {
	event, err := scanEvent(r.db.QueryRowContext(ctx, `
		SELECT id::text, title, description, venue, starts_at, capacity, created_at, updated_at
		FROM events
		WHERE id = $1::uuid
	`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Event{}, domain.ErrEventNotFound
	}
	return event, err
}

func (r *PostgresRepository) Create(ctx context.Context, input domain.EventInput) (domain.Event, error) {
	event, err := scanEvent(r.db.QueryRowContext(ctx, `
		INSERT INTO events (title, description, venue, starts_at, capacity)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id::text, title, description, venue, starts_at, capacity, created_at, updated_at
	`, input.Title, input.Description, input.Venue, input.StartsAt, input.Capacity))
	return event, err
}

func (r *PostgresRepository) Update(ctx context.Context, id string, input domain.EventInput) (domain.Event, error) {
	event, err := scanEvent(r.db.QueryRowContext(ctx, `
		UPDATE events
		SET title = $2, description = $3, venue = $4, starts_at = $5, capacity = $6, updated_at = now()
		WHERE id = $1::uuid
		RETURNING id::text, title, description, venue, starts_at, capacity, created_at, updated_at
	`, id, input.Title, input.Description, input.Venue, input.StartsAt, input.Capacity))
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Event{}, domain.ErrEventNotFound
	}
	return event, err
}

type scanner interface {
	Scan(dest ...any) error
}

func scanEvent(row scanner) (domain.Event, error) {
	var event domain.Event
	err := row.Scan(
		&event.ID,
		&event.Title,
		&event.Description,
		&event.Venue,
		&event.StartsAt,
		&event.Capacity,
		&event.CreatedAt,
		&event.UpdatedAt,
	)
	return event, err
}
