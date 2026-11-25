package domain

import "errors"

var (
	ErrUserNotFound        = errors.New("usuario no encontrado")
	ErrInvalidRefreshToken = errors.New("refresh token inválido")
	ErrExpiredRefreshToken = errors.New("refresh token expirado")
	ErrRefreshTokenReplay  = errors.New("refresh token reutilizado")
)

type User struct {
	ID           string
	Email        string
	PasswordHash string
	Role         string
}
