package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"time"

	"angular-go-starter/backend/internal/auth/domain"
	"angular-go-starter/backend/internal/auth/middleware"
	"angular-go-starter/backend/internal/auth/service"
	"angular-go-starter/backend/internal/auth/token"
)

const refreshCookieName = "refresh_token"

type authService interface {
	Login(ctx context.Context, email, password string) (service.LoginResponse, error)
	Refresh(ctx context.Context, refreshToken string) (service.LoginResponse, error)
	Logout(ctx context.Context, claims *token.Claims, refreshToken string) error
}

type Handler struct {
	auth authService
}

func New(auth authService) *Handler {
	return &Handler{auth: auth}
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()

	var request loginRequest
	if err := decoder.Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "cuerpo JSON inválido")
		return
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		writeError(w, http.StatusBadRequest, "se esperaba un único objeto JSON")
		return
	}

	response, err := h.auth.Login(r.Context(), request.Email, request.Password)
	if errors.Is(err, service.ErrInvalidCredentials) {
		writeError(w, http.StatusUnauthorized, "email o contraseña incorrectos")
		return
	}
	if err != nil {
		log.Printf("login fallido por error interno: %v", err)
		writeError(w, http.StatusInternalServerError, "no se pudo iniciar sesión")
		return
	}
	setRefreshCookie(w, response.RefreshToken, response.RefreshExpiresAt)
	writeJSON(w, http.StatusOK, response)
}

func (h *Handler) Refresh(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie(refreshCookieName)
	if err != nil {
		clearRefreshCookie(w)
		writeError(w, http.StatusUnauthorized, "refresh token inválido")
		return
	}

	response, err := h.auth.Refresh(r.Context(), cookie.Value)
	if errors.Is(err, domain.ErrRefreshTokenReplay) {
		log.Printf("replay de refresh token detectado; familia revocada")
		clearRefreshCookie(w)
		writeError(w, http.StatusUnauthorized, "refresh token inválido")
		return
	}
	if errors.Is(err, domain.ErrInvalidRefreshToken) || errors.Is(err, domain.ErrExpiredRefreshToken) {
		clearRefreshCookie(w)
		writeError(w, http.StatusUnauthorized, "refresh token inválido")
		return
	}
	if err != nil {
		log.Printf("refresh fallido por error interno: %v", err)
		writeError(w, http.StatusInternalServerError, "no se pudo renovar la sesión")
		return
	}

	setRefreshCookie(w, response.RefreshToken, response.RefreshExpiresAt)
	writeJSON(w, http.StatusOK, response)
}

func (h *Handler) Logout(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "no autenticado")
		return
	}
	clearRefreshCookie(w)
	refreshCookie, _ := r.Cookie(refreshCookieName)
	refreshToken := ""
	if refreshCookie != nil {
		refreshToken = refreshCookie.Value
	}
	if err := h.auth.Logout(r.Context(), claims, refreshToken); err != nil {
		log.Printf("logout fallido: %v", err)
		writeError(w, http.StatusInternalServerError, "no se pudo cerrar sesión")
		return
	}
	w.WriteHeader(http.StatusNoContent)
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

func setRefreshCookie(w http.ResponseWriter, value string, expiresAt time.Time) {
	http.SetCookie(w, &http.Cookie{
		Name:     refreshCookieName,
		Value:    value,
		Path:     "/api/auth",
		Expires:  expiresAt,
		MaxAge:   int(time.Until(expiresAt).Seconds()),
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
	})
}

func clearRefreshCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     refreshCookieName,
		Value:    "",
		Path:     "/api/auth",
		Expires:  time.Unix(1, 0).UTC(),
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
	})
}
