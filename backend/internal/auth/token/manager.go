package token

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

var ErrInvalidToken = errors.New("token JWT inválido")

type Claims struct {
	Role string `json:"role"`
	jwt.RegisteredClaims
}

type Manager struct {
	secret []byte
	issuer string
	ttl    time.Duration
}

func NewManager(secret, issuer string, ttl time.Duration) (*Manager, error) {
	if len([]byte(secret)) < 32 {
		return nil, errors.New("JWT_SECRET debe tener al menos 32 bytes")
	}
	if issuer == "" {
		return nil, errors.New("el issuer JWT es obligatorio")
	}
	if ttl <= 0 {
		return nil, errors.New("la duración del token debe ser positiva")
	}
	return &Manager{secret: []byte(secret), issuer: issuer, ttl: ttl}, nil
}

func (m *Manager) Issue(userID, role string) (string, time.Time, error) {
	if userID == "" || role == "" {
		return "", time.Time{}, errors.New("el usuario y el rol son obligatorios para emitir un token")
	}

	jtiBytes := make([]byte, 16)
	if _, err := rand.Read(jtiBytes); err != nil {
		return "", time.Time{}, err
	}

	now := time.Now().UTC()
	expiresAt := now.Add(m.ttl)
	claims := Claims{
		Role: role,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    m.issuer,
			Subject:   userID,
			ID:        hex.EncodeToString(jtiBytes),
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(expiresAt),
		},
	}

	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(m.secret)
	if err != nil {
		return "", time.Time{}, err
	}
	return signed, expiresAt, nil
}

func (m *Manager) Parse(raw string) (*Claims, error) {
	claims := &Claims{}
	parsed, err := jwt.ParseWithClaims(
		raw,
		claims,
		func(parsed *jwt.Token) (any, error) {
			if parsed.Method != jwt.SigningMethodHS256 {
				return nil, ErrInvalidToken
			}
			return m.secret, nil
		},
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithIssuer(m.issuer),
		jwt.WithExpirationRequired(),
		jwt.WithIssuedAt(),
		jwt.WithLeeway(30*time.Second),
	)
	if err != nil || !parsed.Valid || claims.Subject == "" || claims.Role == "" || claims.ID == "" {
		return nil, ErrInvalidToken
	}
	return claims, nil
}
