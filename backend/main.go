package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/redis/go-redis/v9"

	"angular-go-starter/backend/internal/auth/handlers"
	"angular-go-starter/backend/internal/auth/middleware"
	"angular-go-starter/backend/internal/auth/repository"
	"angular-go-starter/backend/internal/auth/service"
	"angular-go-starter/backend/internal/auth/token"
	bookinghandlers "angular-go-starter/backend/internal/bookings/handlers"
	bookingrepository "angular-go-starter/backend/internal/bookings/repository"
	bookingservice "angular-go-starter/backend/internal/bookings/service"
	eventhandlers "angular-go-starter/backend/internal/events/handlers"
	eventrepository "angular-go-starter/backend/internal/events/repository"
	eventservice "angular-go-starter/backend/internal/events/service"
	"angular-go-starter/backend/internal/ratelimit"
)

type healthResponse struct {
	Status  string `json:"status"`
	Service string `json:"service"`
}

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	databaseURL, err := requiredEnv("DATABASE_URL")
	if err != nil {
		return err
	}
	jwtSecret, err := requiredEnv("JWT_SECRET")
	if err != nil {
		return err
	}

	issuer := os.Getenv("JWT_ISSUER")
	if issuer == "" {
		issuer = "angular-go-starter"
	}
	tokenManager, err := token.NewManager(jwtSecret, issuer, 15*time.Minute)
	if err != nil {
		return err
	}

	startupCtx, cancelStartup := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelStartup()

	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		return fmt.Errorf("abrir conexión PostgreSQL: %w", err)
	}
	defer db.Close()
	if err := db.PingContext(startupCtx); err != nil {
		return fmt.Errorf("conectar con PostgreSQL: %w", err)
	}

	redisClient := redis.NewClient(&redis.Options{
		Addr:     envOrDefault("REDIS_ADDR", "localhost:6379"),
		Password: os.Getenv("REDIS_PASSWORD"),
	})
	defer redisClient.Close()
	if err := redisClient.Ping(startupCtx).Err(); err != nil {
		return fmt.Errorf("conectar con Redis: %w", err)
	}

	userRepository := repository.NewPostgresUserRepository(db)
	refreshTokenRepository := repository.NewPostgresRefreshTokenRepository(db)
	revocations := repository.NewRedisRevocationStore(redisClient)
	authService, err := service.New(userRepository, revocations, refreshTokenRepository, tokenManager)
	if err != nil {
		return fmt.Errorf("inicializar servicio de autenticación: %w", err)
	}
	authHandler := handlers.New(authService)
	authMiddleware := middleware.NewAuthenticator(tokenManager, revocations, log.Default())
	loginRateLimit := ratelimit.New(redisClient, log.Default())
	eventsRepository := eventrepository.NewPostgresRepository(db)
	eventsService := eventservice.New(eventsRepository)
	eventsHandler := eventhandlers.New(eventsService)
	bookingsRepository := bookingrepository.NewPostgresRepository(db, redisClient)
	bookingsService := bookingservice.New(bookingsRepository)
	bookingsHandler := bookinghandlers.New(bookingsService)
	requireAuthentication := authMiddleware.RequireAuthentication
	requireAdmin := func(handler http.Handler) http.Handler {
		return authMiddleware.RequireAuthentication(
			authMiddleware.RequireRoles("admin")(handler),
		)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", healthHandler)
	mux.Handle("POST /api/auth/login", loginRateLimit.Wrap(http.HandlerFunc(authHandler.Login)))
	mux.Handle("POST /api/auth/refresh", http.HandlerFunc(authHandler.Refresh))
	mux.Handle("POST /api/auth/logout", authMiddleware.RequireAuthentication(http.HandlerFunc(authHandler.Logout)))
	mux.HandleFunc("GET /api/events", eventsHandler.List)
	mux.HandleFunc("GET /api/events/{id}", eventsHandler.Get)
	mux.Handle("POST /api/events", requireAdmin(http.HandlerFunc(eventsHandler.Create)))
	mux.Handle("PUT /api/events/{id}", requireAdmin(http.HandlerFunc(eventsHandler.Update)))
	mux.Handle("POST /api/bookings", requireAuthentication(http.HandlerFunc(bookingsHandler.Create)))
	mux.Handle("GET /api/bookings/{id}", requireAuthentication(http.HandlerFunc(bookingsHandler.Get)))
	mux.Handle("POST /api/bookings/{id}/confirm", requireAuthentication(http.HandlerFunc(bookingsHandler.Confirm)))
	mux.Handle("DELETE /api/bookings/{id}", requireAuthentication(http.HandlerFunc(bookingsHandler.Cancel)))

	server := &http.Server{
		Addr:              envOrDefault("HTTP_ADDR", ":8080"),
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			log.Printf("cerrar servidor HTTP: %v", err)
		}
	}()

	log.Printf("API escuchando en http://localhost%s", server.Addr)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("iniciar servidor HTTP: %w", err)
	}
	return nil
}

func requiredEnv(name string) (string, error) {
	value := os.Getenv(name)
	if value == "" {
		return "", fmt.Errorf("la variable de entorno %s es obligatoria", name)
	}
	return value, nil
}

func envOrDefault(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

func healthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if err := json.NewEncoder(w).Encode(healthResponse{
		Status:  "ok",
		Service: "backend",
	}); err != nil {
		log.Printf("no se pudo escribir la respuesta de health: %v", err)
	}
}
