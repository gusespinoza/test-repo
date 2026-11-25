package ratelimit

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	window            = time.Minute
	maxLoginBodyBytes = 1 << 20
	ipAttemptLimit    = 5
	emailAttemptLimit = 10
)

const incrementScript = `
local window = tonumber(ARGV[1])
local ipLimit = tonumber(ARGV[2])
local emailLimit = tonumber(ARGV[3])

local function increment(key)
	local count = redis.call("INCR", key)
	local ttl = redis.call("TTL", key)
	if ttl < 0 then
		redis.call("EXPIRE", key, window)
		ttl = window
	end
	return count, ttl
end

local ipCount, ipTTL = increment(KEYS[1])
local retryAfter = 0
local limited = false
if ipCount > ipLimit then
	limited = true
	retryAfter = math.max(retryAfter, ipTTL)
end

if #KEYS > 1 then
	local emailCount, emailTTL = increment(KEYS[2])
	if emailCount > emailLimit then
		limited = true
		retryAfter = math.max(retryAfter, emailTTL)
	end
end

if limited and retryAfter < 1 then
	retryAfter = 1
end
return retryAfter
`

type rateCounter interface {
	Check(ctx context.Context, ip, email string) (time.Duration, error)
}

type redisCounter struct {
	client *redis.Client
}

func (c redisCounter) Check(ctx context.Context, ip, email string) (time.Duration, error) {
	keys := []string{redisKey("ip", ip)}
	if email != "" {
		keys = append(keys, redisKey("email", email))
	}

	retryAfter, err := c.client.Eval(
		ctx,
		incrementScript,
		keys,
		int(window.Seconds()),
		ipAttemptLimit,
		emailAttemptLimit,
	).Int64()
	if err != nil {
		return 0, err
	}
	if retryAfter <= 0 {
		return 0, nil
	}
	if retryAfter < 1 {
		retryAfter = 1
	}
	return time.Duration(retryAfter) * time.Second, nil
}

type Middleware struct {
	counter rateCounter
	logger  *log.Logger
}

func New(client *redis.Client, logger *log.Logger) *Middleware {
	if logger == nil {
		logger = log.Default()
	}
	return &Middleware{
		counter: redisCounter{client: client},
		logger:  logger,
	}
}

func (m *Middleware) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		email := extractEmail(r, m.logger)
		retryAfter, err := m.counter.Check(r.Context(), clientIP(r.RemoteAddr), email)
		if err != nil {
			m.logger.Printf("login rate limiter Redis error; allowing request: %v", err)
			next.ServeHTTP(w, r)
			return
		}
		if retryAfter <= 0 {
			next.ServeHTTP(w, r)
			return
		}

		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Retry-After", strconv.FormatInt(int64(retryAfter.Seconds()), 10))
		w.WriteHeader(http.StatusTooManyRequests)
		if _, err := io.WriteString(w, `{"error":"demasiados intentos de inicio de sesión"}`+"\n"); err != nil {
			m.logger.Printf("no se pudo escribir respuesta 429 del rate limiter: %v", err)
		}
	})
}

func extractEmail(r *http.Request, logger *log.Logger) string {
	body, err := io.ReadAll(io.LimitReader(r.Body, maxLoginBodyBytes+1))
	r.Body = io.NopCloser(io.MultiReader(bytes.NewReader(body), r.Body))
	if err != nil {
		logger.Printf("no se pudo leer el email para el rate limiter: %v", err)
		return ""
	}
	if int64(len(body)) > maxLoginBodyBytes {
		return ""
	}

	var login struct {
		Email string `json:"email"`
	}
	if err := json.Unmarshal(body, &login); err != nil {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(login.Email))
}

func clientIP(remoteAddr string) string {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		host = remoteAddr
	}
	if ip := net.ParseIP(strings.Trim(host, "[]")); ip != nil {
		host = ip.String()
	}
	if host == "" {
		return "unknown"
	}
	return host
}

func redisKey(kind, value string) string {
	sum := sha256.Sum256([]byte(value))
	return "rate-limit:auth:login:" + kind + ":" + hex.EncodeToString(sum[:])
}
