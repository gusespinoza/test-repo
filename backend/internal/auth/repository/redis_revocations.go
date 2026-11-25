package repository

import (
	"context"
	"errors"
	"time"

	"github.com/redis/go-redis/v9"
)

var ErrInvalidRevocation = errors.New("jti y duración de revocación son obligatorios")

type RedisRevocationStore struct {
	client *redis.Client
}

func NewRedisRevocationStore(client *redis.Client) *RedisRevocationStore {
	return &RedisRevocationStore{client: client}
}

func (s *RedisRevocationStore) IsRevoked(ctx context.Context, jti string) (bool, error) {
	count, err := s.client.Exists(ctx, revocationKey(jti)).Result()
	return count > 0, err
}

func (s *RedisRevocationStore) Revoke(ctx context.Context, jti string, ttl time.Duration) error {
	if jti == "" || ttl <= 0 {
		return ErrInvalidRevocation
	}
	return s.client.Set(ctx, revocationKey(jti), "1", ttl).Err()
}

func revocationKey(jti string) string {
	return "auth:revoked:" + jti
}
