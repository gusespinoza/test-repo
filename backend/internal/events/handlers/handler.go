package handlers

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"

	"angular-go-starter/backend/internal/events/domain"
	"angular-go-starter/backend/internal/events/service"
)

const (
	defaultPage  = 1
	defaultLimit = 20
)

type eventService interface {
	List(ctx context.Context, page, limit int) (domain.EventPage, error)
	FindByID(ctx context.Context, id string) (domain.Event, error)
	Create(ctx context.Context, input domain.EventInput) (domain.Event, error)
	Update(ctx context.Context, id string, input domain.EventInput) (domain.Event, error)
}

type Handler struct {
	events eventService
}

func New(events eventService) *Handler {
	return &Handler{events: events}
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	page, limit, err := parsePagination(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "parámetros de paginación inválidos")
		return
	}
	result, err := h.events.List(r.Context(), page, limit)
	if errors.Is(err, domain.ErrInvalidPageRange) {
		writeError(w, http.StatusBadRequest, "parámetros de paginación inválidos")
		return
	}
	if err != nil {
		log.Printf("listar eventos: %v", err)
		writeError(w, http.StatusInternalServerError, "no se pudieron listar los eventos")
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !isUUID(id) {
		writeError(w, http.StatusBadRequest, "id de evento inválido")
		return
	}
	event, err := h.events.FindByID(r.Context(), id)
	if errors.Is(err, domain.ErrEventNotFound) {
		writeError(w, http.StatusNotFound, "evento no encontrado")
		return
	}
	if err != nil {
		log.Printf("obtener evento %q: %v", id, err)
		writeError(w, http.StatusInternalServerError, "no se pudo obtener el evento")
		return
	}
	writeJSON(w, http.StatusOK, event)
}

func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	var input domain.EventInput
	if err := decodeInput(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "cuerpo JSON inválido")
		return
	}
	event, err := h.events.Create(r.Context(), input)
	if errors.Is(err, domain.ErrInvalidEvent) {
		writeError(w, http.StatusBadRequest, "datos del evento inválidos")
		return
	}
	if err != nil {
		log.Printf("crear evento: %v", err)
		writeError(w, http.StatusInternalServerError, "no se pudo crear el evento")
		return
	}
	writeJSON(w, http.StatusCreated, event)
}

func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !isUUID(id) {
		writeError(w, http.StatusBadRequest, "id de evento inválido")
		return
	}
	var input domain.EventInput
	if err := decodeInput(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "cuerpo JSON inválido")
		return
	}
	event, err := h.events.Update(r.Context(), id, input)
	if errors.Is(err, domain.ErrEventNotFound) {
		writeError(w, http.StatusNotFound, "evento no encontrado")
		return
	}
	if errors.Is(err, domain.ErrInvalidEvent) {
		writeError(w, http.StatusBadRequest, "datos del evento inválidos")
		return
	}
	if err != nil {
		log.Printf("actualizar evento %q: %v", id, err)
		writeError(w, http.StatusInternalServerError, "no se pudo actualizar el evento")
		return
	}
	writeJSON(w, http.StatusOK, event)
}

func parsePagination(r *http.Request) (int, int, error) {
	page, limit := defaultPage, defaultLimit
	query := r.URL.Query()
	if query.Has("page") {
		value, err := strconv.Atoi(query.Get("page"))
		if err != nil {
			return 0, 0, err
		}
		page = value
	}
	if query.Has("limit") {
		value, err := strconv.Atoi(query.Get("limit"))
		if err != nil {
			return 0, 0, err
		}
		limit = value
	}
	if page < 1 || limit < 1 || limit > service.MaxPageSize {
		return 0, 0, domain.ErrInvalidPageRange
	}
	maxInt := int(^uint(0) >> 1)
	if page-1 > maxInt/limit {
		return 0, 0, domain.ErrInvalidPageRange
	}
	return page, limit, nil
}

func decodeInput(w http.ResponseWriter, r *http.Request, input *domain.EventInput) error {
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
