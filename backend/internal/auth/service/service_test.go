package service

import (
	"context"
	"crypto/sha256"
	"errors"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"

	"angular-go-starter/backend/internal/auth/domain"
	"angular-go-starter/backend/internal/auth/token"
)

type fakeUserRepository struct {
	user domain.User
	err  error
}

func (r fakeUserRepository) FindByEmail(context.Context, string) (domain.User, error) {
	return r.user, r.err
}

type fakeRevocationStore struct{}

func (fakeRevocationStore) Revoke(context.Context, string, time.Duration) error {
	return nil
}

type fakeRefreshTokenRepository struct {
	createdUserID string
	createdHash   []byte
	createdExpiry time.Time
	rotateUser    domain.User
	rotateErr     error
	rotatedHash   []byte
	nextHash      []byte
	revokedHash   []byte
}

func (r *fakeRefreshTokenRepository) Create(_ context.Context, userID string, hash []byte, expiresAt time.Time) error {
	r.createdUserID = userID
	r.createdHash = append([]byte(nil), hash...)
	r.createdExpiry = expiresAt
	return nil
}

func (r *fakeRefreshTokenRepository) Rotate(
	_ context.Context,
	hash, nextHash []byte,
	_ time.Time,
	_ time.Time,
) (domain.User, error) {
	r.rotatedHash = append([]byte(nil), hash...)
	r.nextHash = append([]byte(nil), nextHash...)
	return r.rotateUser, r.rotateErr
}

func (r *fakeRefreshTokenRepository) RevokeFamily(_ context.Context, hash []byte, _ time.Time) error {
	r.revokedHash = append([]byte(nil), hash...)
	return nil
}

func TestLoginReturnsTokenAndDatabaseRole(t *testing.T) {
	t.Parallel()

	passwordHash, err := bcrypt.GenerateFromPassword([]byte("correct horse battery staple"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	tokens, err := token.NewManager("0123456789abcdef0123456789abcdef", "test", time.Minute)
	if err != nil {
		t.Fatal(err)
	}

	refreshTokens := &fakeRefreshTokenRepository{}
	auth, err := New(fakeUserRepository{user: domain.User{
		ID:           "user-123",
		Email:        "agent@example.com",
		PasswordHash: string(passwordHash),
		Role:         "agent",
	}}, fakeRevocationStore{}, refreshTokens, tokens)
	if err != nil {
		t.Fatal(err)
	}

	response, err := auth.Login(context.Background(), " agent@example.com ", "correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	if response.Role != "agent" || response.TokenType != "Bearer" || response.ExpiresIn <= 0 {
		t.Fatalf("unexpected login response: %+v", response)
	}

	claims, err := tokens.Parse(response.AccessToken)
	if err != nil {
		t.Fatal(err)
	}
	if claims.Subject != "user-123" || claims.Role != "agent" {
		t.Fatalf("unexpected token claims: %+v", claims)
	}
	if response.RefreshToken == "" || refreshTokens.createdUserID != "user-123" {
		t.Fatal("login did not persist a refresh token for the user")
	}
	wantHash := sha256.Sum256([]byte(response.RefreshToken))
	if string(refreshTokens.createdHash) != string(wantHash[:]) {
		t.Fatal("repository did not receive the refresh token hash")
	}
	if time.Until(response.RefreshExpiresAt) > 7*24*time.Hour || time.Until(response.RefreshExpiresAt) <= 0 {
		t.Fatalf("unexpected refresh token expiration: %v", response.RefreshExpiresAt)
	}
}

func TestLoginDoesNotRevealWhetherUserExists(t *testing.T) {
	t.Parallel()

	tokens, err := token.NewManager("0123456789abcdef0123456789abcdef", "test", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	auth, err := New(fakeUserRepository{err: domain.ErrUserNotFound}, fakeRevocationStore{}, &fakeRefreshTokenRepository{}, tokens)
	if err != nil {
		t.Fatal(err)
	}

	_, err = auth.Login(context.Background(), "missing@example.com", "password")
	if !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("expected generic invalid credentials error, got %v", err)
	}
}

func TestRefreshRotatesTokenAndUsesStoredUserRole(t *testing.T) {
	t.Parallel()

	tokens, err := token.NewManager("0123456789abcdef0123456789abcdef", "test", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	refreshTokens := &fakeRefreshTokenRepository{rotateUser: domain.User{
		ID:    "user-123",
		Email: "agent@example.com",
		Role:  "agent",
	}}
	auth, err := New(fakeUserRepository{}, fakeRevocationStore{}, refreshTokens, tokens)
	if err != nil {
		t.Fatal(err)
	}

	oldRefreshToken := "old-refresh-token"
	response, err := auth.Refresh(context.Background(), oldRefreshToken)
	if err != nil {
		t.Fatal(err)
	}
	if response.RefreshToken == "" || response.RefreshToken == oldRefreshToken || response.Role != "agent" {
		t.Fatalf("unexpected rotated session: %+v", response)
	}
	wantOldHash := sha256.Sum256([]byte(oldRefreshToken))
	if string(refreshTokens.rotatedHash) != string(wantOldHash[:]) {
		t.Fatal("repository did not receive the old refresh token hash")
	}
	wantNextHash := sha256.Sum256([]byte(response.RefreshToken))
	if string(refreshTokens.nextHash) != string(wantNextHash[:]) {
		t.Fatal("repository did not receive the new refresh token hash")
	}
}

func TestRefreshPropagatesReplayDetection(t *testing.T) {
	t.Parallel()

	tokens, err := token.NewManager("0123456789abcdef0123456789abcdef", "test", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	auth, err := New(
		fakeUserRepository{},
		fakeRevocationStore{},
		&fakeRefreshTokenRepository{rotateErr: domain.ErrRefreshTokenReplay},
		tokens,
	)
	if err != nil {
		t.Fatal(err)
	}

	_, err = auth.Refresh(context.Background(), "replayed-token")
	if !errors.Is(err, domain.ErrRefreshTokenReplay) {
		t.Fatalf("expected replay error, got %v", err)
	}
}
