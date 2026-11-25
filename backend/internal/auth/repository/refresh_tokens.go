package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"angular-go-starter/backend/internal/auth/domain"
)

type PostgresRefreshTokenRepository struct {
	db *sql.DB
}

func NewPostgresRefreshTokenRepository(db *sql.DB) *PostgresRefreshTokenRepository {
	return &PostgresRefreshTokenRepository{db: db}
}

func (r *PostgresRefreshTokenRepository) Create(
	ctx context.Context,
	userID string,
	tokenHash []byte,
	expiresAt time.Time,
) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO auth_refresh_tokens (user_id, family_id, token_hash, expires_at)
		VALUES ($1, gen_random_uuid(), $2, $3)
	`, userID, tokenHash, expiresAt)
	if err != nil {
		return fmt.Errorf("guardar refresh token: %w", err)
	}
	return nil
}

func (r *PostgresRefreshTokenRepository) Rotate(
	ctx context.Context,
	tokenHash []byte,
	nextTokenHash []byte,
	nextExpiresAt time.Time,
	now time.Time,
) (domain.User, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.User{}, fmt.Errorf("iniciar rotación de refresh token: %w", err)
	}
	defer tx.Rollback()

	var user domain.User
	var familyID string
	var expiresAt time.Time
	var revokedAt sql.NullTime
	err = tx.QueryRowContext(ctx, `
		SELECT u.id::text, u.email, u.role, t.family_id::text, t.expires_at, t.revoked_at
		FROM auth_refresh_tokens AS t
		JOIN auth_users AS u ON u.id = t.user_id
		WHERE t.token_hash = $1
		FOR UPDATE OF t
	`, tokenHash).Scan(
		&user.ID,
		&user.Email,
		&user.Role,
		&familyID,
		&expiresAt,
		&revokedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.User{}, domain.ErrInvalidRefreshToken
	}
	if err != nil {
		return domain.User{}, fmt.Errorf("buscar refresh token: %w", err)
	}

	if revokedAt.Valid {
		if _, err := tx.ExecContext(ctx, `
			UPDATE auth_refresh_tokens
			SET revoked_at = COALESCE(revoked_at, $2)
			WHERE family_id = $1
		`, familyID, now); err != nil {
			return domain.User{}, fmt.Errorf("revocar familia tras replay: %w", err)
		}
		if err := tx.Commit(); err != nil {
			return domain.User{}, fmt.Errorf("confirmar revocación por replay: %w", err)
		}
		return domain.User{}, domain.ErrRefreshTokenReplay
	}
	if !expiresAt.After(now) {
		return domain.User{}, domain.ErrExpiredRefreshToken
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE auth_refresh_tokens
		SET revoked_at = $2
		WHERE token_hash = $1 AND revoked_at IS NULL
	`, tokenHash, now); err != nil {
		return domain.User{}, fmt.Errorf("revocar refresh token anterior: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO auth_refresh_tokens (user_id, family_id, token_hash, expires_at)
		VALUES ($1, $2, $3, $4)
	`, user.ID, familyID, nextTokenHash, nextExpiresAt); err != nil {
		return domain.User{}, fmt.Errorf("guardar refresh token rotado: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return domain.User{}, fmt.Errorf("confirmar rotación de refresh token: %w", err)
	}
	return user, nil
}

func (r *PostgresRefreshTokenRepository) RevokeFamily(
	ctx context.Context,
	tokenHash []byte,
	now time.Time,
) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("iniciar revocación de familia: %w", err)
	}
	defer tx.Rollback()

	var familyID string
	err = tx.QueryRowContext(ctx, `
		SELECT family_id::text
		FROM auth_refresh_tokens
		WHERE token_hash = $1
		FOR UPDATE
	`, tokenHash).Scan(&familyID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("buscar familia de refresh token: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE auth_refresh_tokens
		SET revoked_at = COALESCE(revoked_at, $2)
		WHERE family_id = $1
	`, familyID, now); err != nil {
		return fmt.Errorf("revocar familia de refresh tokens: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("confirmar revocación de familia: %w", err)
	}
	return nil
}
