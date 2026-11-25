package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/redis/go-redis/v9"

	"angular-go-starter/backend/internal/bookings/domain"
)

type PostgresRepository struct {
	db           *sql.DB
	reservations *reservationStore
}

func NewPostgresRepository(db *sql.DB, redisClient redis.UniversalClient) *PostgresRepository {
	return &PostgresRepository{
		db:           db,
		reservations: newReservationStore(redisClient),
	}
}

func (r *PostgresRepository) CreatePending(
	ctx context.Context,
	userID string,
	input domain.CreateInput,
) (domain.Order, error) {
	existing, err := r.findByIdempotencyKey(ctx, userID, input.IdempotencyKey)
	if err != nil {
		return domain.Order{}, err
	}
	if existing != nil {
		return *existing, nil
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Order{}, fmt.Errorf("iniciar transacción de reserva: %w", err)
	}
	defer tx.Rollback()

	var capacity, sold int
	err = tx.QueryRowContext(ctx, `
		SELECT capacity, sold
		FROM events
		WHERE id = $1::uuid
		FOR UPDATE
	`, input.EventID).Scan(&capacity, &sold)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Order{}, domain.ErrEventNotFound
	}
	if err != nil {
		return domain.Order{}, fmt.Errorf("bloquear evento para reservar: %w", err)
	}
	existing, err = findByIdempotencyKeyTx(ctx, tx, userID, input.IdempotencyKey)
	if err != nil {
		return domain.Order{}, err
	}
	if existing != nil {
		if err := tx.Commit(); err != nil {
			return domain.Order{}, fmt.Errorf("cerrar transacción de reserva idempotente: %w", err)
		}
		return *existing, nil
	}

	var orderID string
	if err := tx.QueryRowContext(ctx, `SELECT gen_random_uuid()::text`).Scan(&orderID); err != nil {
		return domain.Order{}, fmt.Errorf("generar id de reserva: %w", err)
	}
	now := time.Now().UTC()
	order := domain.Order{
		ID:             orderID,
		UserID:         userID,
		EventID:        input.EventID,
		Quantity:       input.Quantity,
		Status:         domain.StatusPending,
		IdempotencyKey: input.IdempotencyKey,
		ExpiresAt:      now.Add(reservationTTL),
		CreatedAt:      now,
	}

	if err := r.reservations.reserve(
		ctx,
		input.EventID,
		orderID,
		input.Quantity,
		sold,
		capacity,
		order.ExpiresAt,
	); err != nil {
		return domain.Order{}, err
	}

	_, err = tx.ExecContext(ctx, `
		INSERT INTO orders (
			id, user_id, event_id, quantity, status, idempotency_key, expires_at, created_at
		)
		VALUES ($1::uuid, $2::uuid, $3::uuid, $4, $5, $6, $7, $8)
	`, order.ID, order.UserID, order.EventID, order.Quantity, order.Status, order.IdempotencyKey, order.ExpiresAt, order.CreatedAt)
	if err != nil {
		if rollbackErr := tx.Rollback(); rollbackErr != nil && !errors.Is(rollbackErr, sql.ErrTxDone) {
			err = errors.Join(err, fmt.Errorf("revertir reserva: %w", rollbackErr))
		}
		releaseErr := r.reservations.release(ctx, order.EventID, order.ID)
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			existing, lookupErr := r.findByIdempotencyKey(ctx, userID, input.IdempotencyKey)
			if lookupErr == nil && existing != nil && releaseErr == nil {
				return *existing, nil
			}
			err = errors.Join(err, lookupErr)
		}
		return domain.Order{}, errors.Join(err, releaseErr)
	}
	if err := tx.Commit(); err != nil {
		commitErr := fmt.Errorf("confirmar transacción de reserva: %w", err)
		return domain.Order{}, r.releaseAfterFailure(ctx, order.EventID, order.ID, commitErr)
	}
	return order, nil
}

func (r *PostgresRepository) FindByID(ctx context.Context, id string) (domain.Order, error) {
	order, err := scanOrder(r.db.QueryRowContext(ctx, `
		SELECT id::text, user_id::text, event_id::text, quantity, status,
		       idempotency_key, expires_at, created_at
		FROM orders
		WHERE id = $1::uuid
	`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Order{}, domain.ErrBookingNotFound
	}
	if err != nil {
		return domain.Order{}, fmt.Errorf("buscar reserva: %w", err)
	}
	return order, nil
}

func (r *PostgresRepository) Confirm(ctx context.Context, userID, id string) (domain.Order, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Order{}, fmt.Errorf("iniciar transacción de confirmación: %w", err)
	}
	defer tx.Rollback()

	order, err := scanOrder(tx.QueryRowContext(ctx, `
		SELECT id::text, user_id::text, event_id::text, quantity, status,
		       idempotency_key, expires_at, created_at
		FROM orders
		WHERE id = $1::uuid
		FOR UPDATE
	`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Order{}, domain.ErrBookingNotFound
	}
	if err != nil {
		return domain.Order{}, fmt.Errorf("bloquear reserva para confirmar: %w", err)
	}
	if order.UserID != userID {
		return domain.Order{}, domain.ErrForbidden
	}

	if order.Status == domain.StatusConfirmed {
		if err := tx.Commit(); err != nil {
			return domain.Order{}, fmt.Errorf("cerrar transacción de confirmación: %w", err)
		}
		if err := r.reservations.release(ctx, order.EventID, order.ID); err != nil {
			return order, err
		}
		return order, nil
	}
	if order.Status == domain.StatusCancelled {
		if err := tx.Commit(); err != nil {
			return domain.Order{}, fmt.Errorf("cerrar transacción de reserva cancelada: %w", err)
		}
		if err := r.reservations.release(ctx, order.EventID, order.ID); err != nil {
			return order, err
		}
		return domain.Order{}, domain.ErrBookingConflict
	}
	if order.Status != domain.StatusPending {
		return domain.Order{}, domain.ErrBookingConflict
	}

	var capacity, sold int
	err = tx.QueryRowContext(ctx, `
		SELECT capacity, sold
		FROM events
		WHERE id = $1::uuid
		FOR UPDATE
	`, order.EventID).Scan(&capacity, &sold)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Order{}, domain.ErrEventNotFound
	}
	if err != nil {
		return domain.Order{}, fmt.Errorf("bloquear evento para confirmar: %w", err)
	}
	expired := !order.ExpiresAt.After(time.Now().UTC())
	if expired || order.Quantity > capacity-sold {
		order.Status = domain.StatusCancelled
		if _, err := tx.ExecContext(ctx, `
			UPDATE orders SET status = $2 WHERE id = $1::uuid
		`, order.ID, order.Status); err != nil {
			return domain.Order{}, fmt.Errorf("cancelar reserva no confirmable: %w", err)
		}
		if err := tx.Commit(); err != nil {
			return domain.Order{}, fmt.Errorf("confirmar cancelación de reserva: %w", err)
		}
		if err := r.reservations.release(ctx, order.EventID, order.ID); err != nil {
			return domain.Order{}, err
		}
		if expired {
			return domain.Order{}, domain.ErrBookingExpired
		}
		return domain.Order{}, domain.ErrBookingConflict
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE events SET sold = sold + $2 WHERE id = $1::uuid
	`, order.EventID, order.Quantity); err != nil {
		return domain.Order{}, fmt.Errorf("actualizar entradas vendidas: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE orders SET status = $2 WHERE id = $1::uuid
	`, order.ID, domain.StatusConfirmed); err != nil {
		return domain.Order{}, fmt.Errorf("confirmar reserva: %w", err)
	}
	order.Status = domain.StatusConfirmed
	if err := tx.Commit(); err != nil {
		return domain.Order{}, fmt.Errorf("confirmar transacción de reserva: %w", err)
	}
	if err := r.reservations.release(ctx, order.EventID, order.ID); err != nil {
		return order, err
	}
	return order, nil
}

func (r *PostgresRepository) Cancel(ctx context.Context, userID, id string) (domain.Order, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Order{}, fmt.Errorf("iniciar transacción de cancelación: %w", err)
	}
	defer tx.Rollback()

	order, err := scanOrder(tx.QueryRowContext(ctx, `
		SELECT id::text, user_id::text, event_id::text, quantity, status,
		       idempotency_key, expires_at, created_at
		FROM orders
		WHERE id = $1::uuid
		FOR UPDATE
	`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Order{}, domain.ErrBookingNotFound
	}
	if err != nil {
		return domain.Order{}, fmt.Errorf("bloquear reserva para cancelar: %w", err)
	}
	if order.UserID != userID {
		return domain.Order{}, domain.ErrForbidden
	}
	if order.Status == domain.StatusPending {
		if _, err := tx.ExecContext(ctx, `
			UPDATE orders SET status = $2 WHERE id = $1::uuid
		`, order.ID, domain.StatusCancelled); err != nil {
			return domain.Order{}, fmt.Errorf("cancelar reserva: %w", err)
		}
		order.Status = domain.StatusCancelled
	} else if order.Status == domain.StatusConfirmed {
		var sold int
		if err := tx.QueryRowContext(ctx, `
			SELECT sold FROM events WHERE id = $1::uuid FOR UPDATE
		`, order.EventID).Scan(&sold); err != nil {
			return domain.Order{}, fmt.Errorf("bloquear evento para cancelar: %w", err)
		}
		if sold < order.Quantity {
			return domain.Order{}, fmt.Errorf("ventas inconsistentes al cancelar orden %s", order.ID)
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE events SET sold = sold - $2 WHERE id = $1::uuid
		`, order.EventID, order.Quantity); err != nil {
			return domain.Order{}, fmt.Errorf("liberar entradas vendidas: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE orders SET status = $2 WHERE id = $1::uuid
		`, order.ID, domain.StatusCancelled); err != nil {
			return domain.Order{}, fmt.Errorf("cancelar reserva confirmada: %w", err)
		}
		order.Status = domain.StatusCancelled
	}
	if err := tx.Commit(); err != nil {
		return domain.Order{}, fmt.Errorf("confirmar transacción de cancelación: %w", err)
	}
	if err := r.reservations.release(ctx, order.EventID, order.ID); err != nil {
		return order, err
	}
	return order, nil
}

func (r *PostgresRepository) findByIdempotencyKey(
	ctx context.Context,
	userID, key string,
) (*domain.Order, error) {
	order, err := scanOrder(r.db.QueryRowContext(ctx, `
		SELECT id::text, user_id::text, event_id::text, quantity, status,
		       idempotency_key, expires_at, created_at
		FROM orders
		WHERE user_id = $1::uuid AND idempotency_key = $2
	`, userID, key))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("buscar clave de idempotencia: %w", err)
	}
	return &order, nil
}

func findByIdempotencyKeyTx(
	ctx context.Context,
	tx *sql.Tx,
	userID, key string,
) (*domain.Order, error) {
	order, err := scanOrder(tx.QueryRowContext(ctx, `
		SELECT id::text, user_id::text, event_id::text, quantity, status,
		       idempotency_key, expires_at, created_at
		FROM orders
		WHERE user_id = $1::uuid AND idempotency_key = $2
	`, userID, key))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("comprobar clave de idempotencia: %w", err)
	}
	return &order, nil
}

func (r *PostgresRepository) releaseAfterFailure(
	ctx context.Context,
	eventID, reservationID string,
	cause error,
) error {
	if err := r.reservations.release(ctx, eventID, reservationID); err != nil {
		return errors.Join(cause, err)
	}
	return cause
}

type scanner interface {
	Scan(dest ...any) error
}

func scanOrder(row scanner) (domain.Order, error) {
	var order domain.Order
	var status string
	err := row.Scan(
		&order.ID,
		&order.UserID,
		&order.EventID,
		&order.Quantity,
		&status,
		&order.IdempotencyKey,
		&order.ExpiresAt,
		&order.CreatedAt,
	)
	order.Status = domain.Status(status)
	return order, err
}
