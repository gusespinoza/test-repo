package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"

	"angular-go-starter/backend/internal/bookings/domain"
)

const reservationTTL = 10 * time.Minute

var reserveScript = redis.NewScript(`
local now = redis.call('TIME')
local now_ms = now[1] * 1000 + math.floor(now[2] / 1000)
local expired = redis.call('ZRANGEBYSCORE', KEYS[2], '-inf', now_ms)
local current = tonumber(redis.call('GET', KEYS[1]) or '0')

for _, reservation_id in ipairs(expired) do
  local quantity = tonumber(redis.call('HGET', KEYS[3], reservation_id) or '0')
  if quantity > 0 and current > 0 then
    if current <= quantity then
      redis.call('DEL', KEYS[1])
      current = 0
    else
      current = redis.call('DECRBY', KEYS[1], quantity)
    end
  end
  redis.call('ZREM', KEYS[2], reservation_id)
  redis.call('HDEL', KEYS[3], reservation_id)
end

local capacity = tonumber(ARGV[1])
local sold = tonumber(ARGV[2])
local quantity = tonumber(ARGV[3])
if sold + current + quantity > capacity then
  return 0
end

current = redis.call('INCRBY', KEYS[1], quantity)
redis.call('ZADD', KEYS[2], ARGV[4], ARGV[5])
redis.call('HSET', KEYS[3], ARGV[5], quantity)
redis.call('EXPIRE', KEYS[1], ARGV[6])
redis.call('EXPIRE', KEYS[2], ARGV[6])
redis.call('EXPIRE', KEYS[3], ARGV[6])
return current
`)

var releaseScript = redis.NewScript(`
local now = redis.call('TIME')
local now_ms = now[1] * 1000 + math.floor(now[2] / 1000)
local expired = redis.call('ZRANGEBYSCORE', KEYS[2], '-inf', now_ms)
local current = tonumber(redis.call('GET', KEYS[1]) or '0')

for _, reservation_id in ipairs(expired) do
  local quantity = tonumber(redis.call('HGET', KEYS[3], reservation_id) or '0')
  if quantity > 0 and current > 0 then
    if current <= quantity then
      redis.call('DEL', KEYS[1])
      current = 0
    else
      current = redis.call('DECRBY', KEYS[1], quantity)
    end
  end
  redis.call('ZREM', KEYS[2], reservation_id)
  redis.call('HDEL', KEYS[3], reservation_id)
end

local quantity = tonumber(redis.call('HGET', KEYS[3], ARGV[1]) or '0')
if quantity <= 0 then
  return 0
end

if current > 0 then
  if current <= quantity then
    redis.call('DEL', KEYS[1])
  else
    redis.call('DECRBY', KEYS[1], quantity)
  end
end
redis.call('ZREM', KEYS[2], ARGV[1])
redis.call('HDEL', KEYS[3], ARGV[1])
return quantity
`)

type reservationStore struct {
	client redis.UniversalClient
}

func newReservationStore(client redis.UniversalClient) *reservationStore {
	return &reservationStore{client: client}
}

func (s *reservationStore) reserve(
	ctx context.Context,
	eventID, reservationID string,
	quantity, sold, capacity int,
	expiresAt time.Time,
) error {
	result, err := reserveScript.Run(
		ctx,
		s.client,
		reservationKeys(eventID),
		capacity,
		sold,
		quantity,
		expiresAt.UnixMilli(),
		reservationID,
		int(reservationTTL.Seconds()),
	).Int64()
	if err != nil {
		return fmt.Errorf("%w: reservar entradas en Redis: %v", domain.ErrRedisUnavailable, err)
	}
	if result == 0 {
		return domain.ErrBookingConflict
	}
	return nil
}

func (s *reservationStore) release(ctx context.Context, eventID, reservationID string) error {
	_, err := releaseScript.Run(
		ctx,
		s.client,
		reservationKeys(eventID),
		reservationID,
	).Int64()
	if err != nil {
		return fmt.Errorf("%w: liberar reserva en Redis: %v", domain.ErrRedisUnavailable, err)
	}
	return nil
}

func reservationKeys(eventID string) []string {
	return []string{
		"booking:lock:" + eventID,
		"booking:reservations:" + eventID,
		"booking:reservation-quantities:" + eventID,
	}
}
