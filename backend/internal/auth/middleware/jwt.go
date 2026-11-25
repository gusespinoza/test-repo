package middleware

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strings"

	"angular-go-starter/backend/internal/auth/token"
)

type RevocationChecker interface {
	IsRevoked(ctx context.Context, jti string) (bool, error)
}

type Authenticator struct {
	tokens      *token.Manager
	revocations RevocationChecker
	logger      *log.Logger
}

type contextKey struct{}

func NewAuthenticator(tokens *token.Manager, revocations RevocationChecker, logger *log.Logger) *Authenticator {
	return &Authenticator{tokens: tokens, revocations: revocations, logger: logger}
}

func (a *Authenticator) RequireAuthentication(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		parts := strings.Fields(r.Header.Get("Authorization"))
		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
			writeError(w, http.StatusUnauthorized, "token de acceso requerido")
			return
		}

		claims, err := a.tokens.Parse(parts[1])
		if err != nil {
			writeError(w, http.StatusUnauthorized, "token de acceso inválido")
			return
		}
		revoked, err := a.revocations.IsRevoked(r.Context(), claims.ID)
		if err != nil {
			a.logger.Printf("no se pudo comprobar la revocación del token: %v", err)
			writeError(w, http.StatusServiceUnavailable, "servicio de autenticación no disponible")
			return
		}
		if revoked {
			writeError(w, http.StatusUnauthorized, "token de acceso revocado")
			return
		}

		ctx := context.WithValue(r.Context(), contextKey{}, claims)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (a *Authenticator) RequireRoles(roles ...string) func(http.Handler) http.Handler {
	allowed := make(map[string]struct{}, len(roles))
	for _, role := range roles {
		allowed[role] = struct{}{}
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims, ok := ClaimsFromContext(r.Context())
			if !ok {
				writeError(w, http.StatusUnauthorized, "no autenticado")
				return
			}
			if _, ok := allowed[claims.Role]; !ok {
				writeError(w, http.StatusForbidden, "rol sin permisos")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func ClaimsFromContext(ctx context.Context) (*token.Claims, bool) {
	claims, ok := ctx.Value(contextKey{}).(*token.Claims)
	return claims, ok
}

func writeError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(map[string]string{"error": message}); err != nil {
		log.Printf("no se pudo escribir respuesta de autenticación: %v", err)
	}
}
