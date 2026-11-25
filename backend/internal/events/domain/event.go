package domain

import (
	"errors"
	"time"
)

var (
	ErrEventNotFound    = errors.New("evento no encontrado")
	ErrInvalidEvent     = errors.New("evento inválido")
	ErrInvalidPageRange = errors.New("paginación inválida")
)

type Event struct {
	ID          string    `json:"id"`
	Title       string    `json:"title"`
	Description string    `json:"description"`
	Venue       string    `json:"venue"`
	StartsAt    time.Time `json:"starts_at"`
	Capacity    int       `json:"capacity"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type EventInput struct {
	Title       string    `json:"title"`
	Description string    `json:"description"`
	Venue       string    `json:"venue"`
	StartsAt    time.Time `json:"starts_at"`
	Capacity    int       `json:"capacity"`
}

type EventPage struct {
	Items []Event `json:"items"`
	Page  int     `json:"page"`
	Limit int     `json:"limit"`
	Total int64   `json:"total"`
}
