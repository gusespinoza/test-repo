package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"

	"angular-go-starter/backend/internal/auth/domain"
	"angular-go-starter/backend/internal/auth/token"
)

const refreshTokenTTL = 7 * 24 * time.Hour

var ErrInvalidCredentials = errors.New("credenciales inválidas")

type UserRepository interface {
	FindByEmail(ctx context.Context, email string) (domain.User, error)
}

type RevocationStore interface {
	Revoke(ctx context.Context, jti string, ttl time.Duration) error
}

type RefreshTokenRepository interface {
	Create(ctx context.Context, userID string, tokenHash []byte, expiresAt time.Time) error
	Rotate(ctx context.Context, tokenHash, nextTokenHash []byte, nextExpiresAt, now time.Time) (domain.User, error)
	RevokeFamily(ctx context.Context, tokenHash []byte, now time.Time) error
}

type LoginResponse struct {
	AccessToken      string    `json:"access_token"`
	TokenType        string    `json:"token_type"`
	ExpiresIn        int64     `json:"expires_in"`
	Role             string    `json:"role"`
	RefreshToken     string    `json:"-"`
	RefreshExpiresAt time.Time `json:"-"`
}

type Service struct {
	users         UserRepository
	revocations   RevocationStore
	refreshTokens RefreshTokenRepository
	tokens        *token.Manager
	dummyHash     []byte
}

func New(
	users UserRepository,
	revocations RevocationStore,
	refreshTokens RefreshTokenRepository,
	tokens *token.Manager,
) (*Service, error) {
	dummyPassword := make([]byte, 32)
	if _, err := rand.Read(dummyPassword); err != nil {
		return nil, fmt.Errorf("generar contraseña ficticia para login: %w", err)
	}
	dummyHash, err := bcrypt.GenerateFromPassword(dummyPassword, bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("generar hash ficticio para login: %w", err)
	}
	return &Service{
		users:         users,
		revocations:   revocations,
		refreshTokens: refreshTokens,
		tokens:        tokens,
		dummyHash:     dummyHash,
	}, nil
}

func (s *Service) Login(ctx context.Context, email, password string) (LoginResponse, error) {
	email = strings.TrimSpace(email)
	if email == "" || password == "" {
		return LoginResponse{}, ErrInvalidCredentials
	}

	user, err := s.users.FindByEmail(ctx, email)
	if errors.Is(err, domain.ErrUserNotFound) {
		_ = bcrypt.CompareHashAndPassword(s.dummyHash, []byte(password))
		return LoginResponse{}, ErrInvalidCredentials
	}
	if err != nil {
		return LoginResponse{}, fmt.Errorf("buscar usuario para login: %w", err)
	}
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)); err != nil {
		return LoginResponse{}, ErrInvalidCredentials
	}

	accessToken, expiresAt, err := s.tokens.Issue(user.ID, user.Role)
	if err != nil {
		return LoginResponse{}, fmt.Errorf("emitir token de acceso: %w", err)
	}

	refreshToken, refreshHash, err := newRefreshToken()
	if err != nil {
		return LoginResponse{}, fmt.Errorf("generar refresh token: %w", err)
	}
	refreshExpiresAt := time.Now().UTC().Add(refreshTokenTTL)
	if err := s.refreshTokens.Create(ctx, user.ID, refreshHash, refreshExpiresAt); err != nil {
		return LoginResponse{}, fmt.Errorf("guardar refresh token: %w", err)
	}

	return LoginResponse{
		AccessToken:      accessToken,
		TokenType:        "Bearer",
		ExpiresIn:        int64(time.Until(expiresAt).Seconds()),
		Role:             user.Role,
		RefreshToken:     refreshToken,
		RefreshExpiresAt: refreshExpiresAt,
	}, nil
}

func (s *Service) Refresh(ctx context.Context, refreshToken string) (LoginResponse, error) {
	if refreshToken == "" {
		return LoginResponse{}, domain.ErrInvalidRefreshToken
	}

	nextRefreshToken, nextTokenHash, err := newRefreshToken()
	if err != nil {
		return LoginResponse{}, fmt.Errorf("generar refresh token rotado: %w", err)
	}
	now := time.Now().UTC()
	refreshExpiresAt := now.Add(refreshTokenTTL)
	user, err := s.refreshTokens.Rotate(
		ctx,
		hashRefreshToken(refreshToken),
		nextTokenHash,
		refreshExpiresAt,
		now,
	)
	if err != nil {
		return LoginResponse{}, err
	}

	accessToken, expiresAt, err := s.tokens.Issue(user.ID, user.Role)
	if err != nil {
		return LoginResponse{}, fmt.Errorf("emitir token de acceso renovado: %w", err)
	}
	return LoginResponse{
		AccessToken:      accessToken,
		TokenType:        "Bearer",
		ExpiresIn:        int64(time.Until(expiresAt).Seconds()),
		Role:             user.Role,
		RefreshToken:     nextRefreshToken,
		RefreshExpiresAt: refreshExpiresAt,
	}, nil
}

func (s *Service) Logout(ctx context.Context, claims *token.Claims, refreshToken string) error {
	if claims == nil || claims.ID == "" || claims.ExpiresAt == nil {
		return token.ErrInvalidToken
	}

	var failures []error
	if refreshToken != "" {
		if err := s.refreshTokens.RevokeFamily(ctx, hashRefreshToken(refreshToken), time.Now().UTC()); err != nil {
			failures = append(failures, fmt.Errorf("revocar familia de refresh tokens: %w", err))
		}
	}
	if ttl := time.Until(claims.ExpiresAt.Time); ttl > 0 {
		if err := s.revocations.Revoke(ctx, claims.ID, ttl); err != nil {
			failures = append(failures, fmt.Errorf("revocar access token: %w", err))
		}
	}
	return errors.Join(failures...)
}

func newRefreshToken() (string, []byte, error) {
	entropy := make([]byte, 32)
	if _, err := rand.Read(entropy); err != nil {
		return "", nil, err
	}
	raw := base64.RawURLEncoding.EncodeToString(entropy)
	return raw, hashRefreshToken(raw), nil
}

func hashRefreshToken(raw string) []byte {
	hash := sha256.Sum256([]byte(raw))
	return hash[:]
}
