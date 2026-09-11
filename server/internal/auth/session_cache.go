package auth

import (
	"context"
	"errors"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/redis/go-redis/v9"
)

// Session JWT refusals. Callers map them to their own transport error.
var (
	ErrInvalidToken   = errors.New("invalid token")
	ErrInvalidClaims  = errors.New("invalid claims")
	ErrSessionRevoked = errors.New("session revoked")
)

// ParseSessionJWT validates a session JWT (HMAC signature, expiry, non-empty
// sub) and refuses one minted before the user's sessions were revoked (K60).
// Every JWT entry point (HTTP, daemon, realtime) goes through it so a new
// session rule cannot be added to one path and forgotten on the others.
// revocations may be nil, which skips the revocation check.
func ParseSessionJWT(ctx context.Context, tokenString string, revocations *SessionRevocations) (sub, email string, err error) {
	token, err := jwt.Parse(tokenString, func(token *jwt.Token) (any, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, jwt.ErrSignatureInvalid
		}
		return JWTSecret(), nil
	})
	if err != nil || !token.Valid {
		return "", "", errors.Join(ErrInvalidToken, err)
	}
	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return "", "", ErrInvalidClaims
	}
	sub, ok = claims["sub"].(string)
	if !ok || strings.TrimSpace(sub) == "" {
		return "", "", ErrInvalidClaims
	}
	email, _ = claims["email"].(string)
	iat := time.Time{}
	if v, ok := claims["iat"].(float64); ok {
		iat = time.Unix(int64(v), 0)
	}
	if revocations.RefusesTokenIssuedAt(ctx, sub, iat) {
		return "", "", ErrSessionRevoked
	}
	return sub, email, nil
}

// SessionRevocations (K60) answers, per user, the instant before which a
// JWT is refused. SCIM deprovisioning stamps it synchronously; the auth
// middleware checks it on every JWT. Redis keeps the read off the database
// on the hot path; without Redis an in-process map does the same for one
// server.
type SessionRevocations struct {
	rdb    *redis.Client
	load   func(ctx context.Context, userID string) (time.Time, bool, error)
	mu     sync.Mutex
	local  map[string]localRevocation
	ttl    time.Duration
	prefix string
}

type localRevocation struct {
	at      time.Time
	valid   bool
	expires time.Time
}

const sessionRevocationTTL = 5 * time.Minute

// NewSessionRevocations builds the cache; load reads the user's
// sessions_invalidated_at from the database.
func NewSessionRevocations(rdb *redis.Client, load func(ctx context.Context, userID string) (time.Time, bool, error)) *SessionRevocations {
	return &SessionRevocations{rdb: rdb, load: load, local: map[string]localRevocation{}, ttl: sessionRevocationTTL, prefix: "mul:auth:sessinv:"}
}

// RefusesTokenIssuedAt reports whether a token issued at iat is revoked for
// the user. A lookup failure fails open with a warning: a dead cache must not
// log everyone out, and the stamp is checked again on the next request.
func (c *SessionRevocations) RefusesTokenIssuedAt(ctx context.Context, userID string, iat time.Time) bool {
	if c == nil {
		return false
	}
	at, valid, ok := c.cached(ctx, userID)
	if !ok {
		var err error
		at, valid, err = c.load(ctx, userID)
		if err != nil {
			slog.Warn("session revocations: load failed; allowing", "error", err)
			return false
		}
		c.store(ctx, userID, at, valid)
	}
	return valid && !iat.After(at)
}

// Invalidate records a revocation instant so the next request sees it.
func (c *SessionRevocations) Invalidate(ctx context.Context, userID string, at time.Time) {
	if c == nil {
		return
	}
	c.store(ctx, userID, at, true)
}

func (c *SessionRevocations) cached(ctx context.Context, userID string) (time.Time, bool, bool) {
	if c.rdb != nil {
		v, err := c.rdb.Get(ctx, c.prefix+userID).Result()
		if err == nil {
			if v == "" {
				return time.Time{}, false, true
			}
			n, perr := strconv.ParseInt(v, 10, 64)
			if perr == nil {
				return time.Unix(n, 0), true, true
			}
		} else if !errors.Is(err, redis.Nil) {
			slog.Warn("session revocations: redis get failed", "error", err)
		}
		return time.Time{}, false, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.local[userID]
	if !ok || time.Now().After(entry.expires) {
		return time.Time{}, false, false
	}
	return entry.at, entry.valid, true
}

func (c *SessionRevocations) store(ctx context.Context, userID string, at time.Time, valid bool) {
	if c.rdb != nil {
		v := ""
		if valid {
			v = strconv.FormatInt(at.Unix(), 10)
		}
		if err := c.rdb.Set(ctx, c.prefix+userID, v, c.ttl).Err(); err != nil {
			slog.Warn("session revocations: redis set failed", "error", err)
		}
		return
	}
	c.mu.Lock()
	c.local[userID] = localRevocation{at: at, valid: valid, expires: time.Now().Add(c.ttl)}
	c.mu.Unlock()
}
