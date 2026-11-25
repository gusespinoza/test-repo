package domain

import (
	"errors"
	"time"
)

var (
	ErrInvalidBooking   = errors.New("reserva inválida")
	ErrEventNotFound    = errors.New("evento no encontrado")
	ErrBookingNotFound  = errors.New("reserva no encontrada")
	ErrBookingConflict  = errors.New("reserva en conflicto")
	ErrBookingExpired   = errors.New("reserva expirada")
	ErrForbidden        = errors.New("sin permiso para acceder a la reserva")
	ErrRedisUnavailable = errors.New("servicio de reservas no disponible")
)

type Status string

const (
	StatusPending   Status = "pending"
	StatusConfirmed Status = "confirmed"
	StatusCancelled Status = "cancelled"
)

type Order struct {
	ID             string    `json:"id"`
	UserID         string    `json:"user_id"`
	EventID        string    `json:"event_id"`
	Quantity       int       `json:"quantity"`
	Status         Status    `json:"status"`
	IdempotencyKey string    `json:"idempotency_key"`
	ExpiresAt      time.Time `json:"expires_at"`
	CreatedAt      time.Time `json:"created_at"`
}

type CreateInput struct {
	EventID        string `json:"event_id"`
	Quantity       int    `json:"quantity"`
	IdempotencyKey string `json:"idempotency_key"`
}
