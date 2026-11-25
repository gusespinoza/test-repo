package repository

import (
	"context"
	"database/sql"
	"errors"

	"angular-go-starter/backend/internal/auth/domain"
)

type PostgresUserRepository struct {
	db *sql.DB
}

func NewPostgresUserRepository(db *sql.DB) *PostgresUserRepository {
	return &PostgresUserRepository{db: db}
}

func (r *PostgresUserRepository) FindByEmail(ctx context.Context, email string) (domain.User, error) {
	var user domain.User
	err := r.db.QueryRowContext(ctx, `
		SELECT id::text, email, password_hash, role
		FROM auth_users
		WHERE lower(email) = lower($1)
	`, email).Scan(&user.ID, &user.Email, &user.PasswordHash, &user.Role)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.User{}, domain.ErrUserNotFound
	}
	if err != nil {
		return domain.User{}, err
	}
	return user, nil
}
