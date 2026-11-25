package handlers

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"strings"

	"angular-go-starter/backend/internal/auth/middleware"
	"angular-go-starter/backend/internal/bookings/domain"
)

type bookingService interface {
	Create(ctx context.Context, userID string, input domain.CreateInput) (domain.Order, error)
	Get(ctx context.Context, userID, role, id string) (domain.Order, error)
	Confirm(ctx context.Context, userID, id string) (domain.Order, error)
	Cancel(ctx context.Context, userID, id string) (domain.Order, error)
}

type Handler struct {
	bookings bookingService
}

func New(bookings bookingService) *Handler {
	return &Handler{bookings: bookings}
}

func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "no autenticado")
		return
	}
	var input domain.CreateInput
	if err := decodeInput(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "cuerpo JSON inválido")
		return
	}
	if !isUUID(input.EventID) {
		writeError(w, http.StatusBadRequest, "event_id inválido")
		return
	}
	order, err := h.bookings.Create(r.Context(), claims.Subject, input)
	if errors.Is(err, domain.ErrInvalidBooking) {
		writeError(w, http.StatusBadRequest, "event_id, quantity e idempotency_key son obligatorios; quantity debe ser mayor que cero")
		return
	}
	if errors.Is(err, domain.ErrEventNotFound) {
		writeError(w, http.StatusNotFound, "evento no encontrado")
		return
	}
	if errors.Is(err, domain.ErrBookingConflict) {
		writeError(w, http.StatusConflict, "no hay suficientes entradas disponibles")
		return
	}
	if err != nil {
		if errors.Is(err, domain.ErrRedisUnavailable) {
			writeServiceError(w, err)
			return
		}
		writeInternalError(w, err, "no se pudo crear la reserva")
		return
	}
	writeJSON(w, http.StatusCreated, order)
}

func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "no autenticado")
		return
	}
	id := r.PathValue("id")
	if !isUUID(id) {
		writeError(w, http.StatusBadRequest, "id de reserva inválido")
		return
	}
	order, err := h.bookings.Get(r.Context(), claims.Subject, claims.Role, id)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, order)
}

func (h *Handler) Confirm(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "no autenticado")
		return
	}
	id := r.PathValue("id")
	if !isUUID(id) {
		writeError(w, http.StatusBadRequest, "id de reserva inválido")
		return
	}
	order, err := h.bookings.Confirm(r.Context(), claims.Subject, id)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, order)
}

func (h *Handler) Cancel(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "no autenticado")
		return
	}
	id := r.PathValue("id")
	if !isUUID(id) {
		writeError(w, http.StatusBadRequest, "id de reserva inválido")
		return
	}
	order, err := h.bookings.Cancel(r.Context(), claims.Subject, id)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, order)
}

func decodeInput(w http.ResponseWriter, r *http.Request, input *domain.CreateInput) error {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(input); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return errors.New("se esperaba un único objeto JSON")
	}
	return nil
}

func isUUID(id string) bool {
	if len(id) != 36 || id[8] != '-' || id[13] != '-' || id[18] != '-' || id[23] != '-' {
		return false
	}
	_, err := hex.DecodeString(strings.ReplaceAll(id, "-", ""))
	return err == nil
}

func writeServiceError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, domain.ErrBookingNotFound), errors.Is(err, domain.ErrEventNotFound):
		writeError(w, http.StatusNotFound, "reserva o evento no encontrado")
	case errors.Is(err, domain.ErrForbidden):
		writeError(w, http.StatusForbidden, "no tienes permiso para acceder a esta reserva")
	case errors.Is(err, domain.ErrBookingConflict), errors.Is(err, domain.ErrBookingExpired):
		writeError(w, http.StatusConflict, "la reserva ya no puede completarse")
	case errors.Is(err, domain.ErrRedisUnavailable):
		log.Printf("servicio de reservas no disponible: %v", err)
		writeError(w, http.StatusServiceUnavailable, "servicio de reservas no disponible")
	default:
		writeInternalError(w, err, "no se pudo procesar la reserva")
	}
}

func writeInternalError(w http.ResponseWriter, err error, message string) {
	log.Printf("%s: %v", message, err)
	writeError(w, http.StatusInternalServerError, message)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		log.Printf("no se pudo escribir respuesta HTTP: %v", err)
	}
}
