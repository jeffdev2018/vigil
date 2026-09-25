package middleware

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

// rateLimitScript atomically increments the counter and sets the TTL on
// first access. Using a Lua script ensures INCR and EXPIRE cannot be
// split by a network failure — if INCR succeeds the TTL is guaranteed
// to be set, preventing a stuck key that acts as a permanent ban.
var rateLimitScript = redis.NewScript(`
local count = redis.call('INCR', KEYS[1])
if count == 1 then
    redis.call('EXPIRE', KEYS[1], ARGV[1])
end
return count
`)

// ParseTrustedProxies parses a comma-separated list of CIDRs into a
// slice of *net.IPNet. Invalid entries are warned and skipped.
// Returns nil if raw is empty (default: never trust X-Forwarded-For).
func ParseTrustedProxies(raw string) []*net.IPNet {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	var nets []*net.IPNet
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		_, cidr, err := net.ParseCIDR(p)
		if err != nil {
			slog.Warn("ratelimit: invalid trusted proxy CIDR, skipping", "cidr", p, "error", err)
			continue
		}
		nets = append(nets, cidr)
	}
	return nets
}

// RateLimit returns a per-IP fixed-window rate limiter backed by Redis.
// If rdb is nil and isProduction is false, the middleware is a no-op
// (fail-open) — this is the historical dev/test behavior and is unchanged.
// If rdb is nil and isProduction is true, requests fall back to an
// in-memory per-process limiter (see inMemoryRateLimiter) so production auth
// endpoints stay protected even when REDIS_URL is missing.
//
// trustedProxies controls X-Forwarded-For handling: when the direct
// connection (RemoteAddr) originates from a CIDR in the list, the
// rightmost non-trusted IP in the XFF chain is used as the client IP.
// When the list is empty (default), XFF is never consulted — only
// RemoteAddr is used. This matches the project's conservative trust
// model (see health_realtime.go).
func RateLimit(rdb redis.UniversalClient, limit int, window time.Duration, trustedProxies []*net.IPNet, isProduction bool) func(http.Handler) http.Handler {
	if rdb == nil && !isProduction {
		return func(next http.Handler) http.Handler {
			return next
		}
	}

	var fallback *inMemoryRateLimiter
	if rdb == nil {
		fallback = newInMemoryRateLimiter(limit, window)
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := extractIP(r, trustedProxies)

			if fallback != nil {
				allowed, retryAfter := fallback.allow(rateLimitKey(r.URL.Path, ip))
				if !allowed {
					writeTooManyRequests(w, int(retryAfter.Seconds()))
					return
				}
				next.ServeHTTP(w, r)
				return
			}

			key := rateLimitKey(r.URL.Path, ip)
			ctx := r.Context()

			count, err := rateLimitScript.Run(ctx, rdb, []string{key}, int(window.Seconds())).Int64()
			if err != nil {
				slog.Warn("ratelimit: redis error; allowing request", "error", err, "ip", ip)
				next.ServeHTTP(w, r)
				return
			}
			if count > int64(limit) {
				writeTooManyRequests(w, int(window.Seconds()))
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

func writeTooManyRequests(w http.ResponseWriter, retryAfterSeconds int) {
	w.Header().Set("Retry-After", fmt.Sprintf("%d", retryAfterSeconds))
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusTooManyRequests)
	json.NewEncoder(w).Encode(map[string]string{"error": "too many requests"})
}

// inMemoryRateLimiter is a per-process, per-IP fixed-window limiter used only
// as a fallback when REDIS_URL is not configured in production. It does not
// coordinate across replicas or survive a restart, unlike the Redis path.
//
// ponytail: per-process, single global mutex; move to Redis (set REDIS_URL)
// if multi-replica coordination or throughput ever makes this a bottleneck.
type inMemoryRateLimiter struct {
	mu     sync.Mutex
	limit  int
	window time.Duration
	counts map[string]*inMemoryWindow
}

type inMemoryWindow struct {
	count   int
	resetAt time.Time
}

// inMemoryRateLimiterMaxKeys bounds the map so a process left running for a
// long time under a wide-open IP range (or REDIS_URL misconfiguration) can't
// grow this unbounded. Expired windows are swept opportunistically once the
// map crosses this size, rather than on a timer, to avoid a background
// goroutine per limiter instance.
const inMemoryRateLimiterMaxKeys = 10000

func newInMemoryRateLimiter(limit int, window time.Duration) *inMemoryRateLimiter {
	return &inMemoryRateLimiter{
		limit:  limit,
		window: window,
		counts: make(map[string]*inMemoryWindow),
	}
}

// allow reports whether the request under key is within the limit, and the
// duration until the current window resets (for the Retry-After header).
func (l *inMemoryRateLimiter) allow(key string) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	if len(l.counts) >= inMemoryRateLimiterMaxKeys {
		for k, w := range l.counts {
			if now.After(w.resetAt) {
				delete(l.counts, k)
			}
		}
	}

	w, ok := l.counts[key]
	if !ok || now.After(w.resetAt) {
		w = &inMemoryWindow{resetAt: now.Add(l.window)}
		l.counts[key] = w
	}
	w.count++

	return w.count <= l.limit, time.Until(w.resetAt)
}

// extractIP determines the client IP for rate limiting purposes.
// It only honors X-Forwarded-For when RemoteAddr is from a trusted proxy.
func extractIP(r *http.Request, trustedProxies []*net.IPNet) string {
	remoteHost, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		remoteHost = r.RemoteAddr
	}

	if len(trustedProxies) > 0 {
		remoteIP := net.ParseIP(remoteHost)
		if remoteIP != nil && isTrustedProxy(remoteIP, trustedProxies) {
			if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
				// Walk right-to-left: the rightmost non-trusted entry is
				// the last hop before the trusted proxy chain.
				parts := strings.Split(xff, ",")
				for i := len(parts) - 1; i >= 0; i-- {
					candidate := net.ParseIP(strings.TrimSpace(parts[i]))
					if candidate != nil && !isTrustedProxy(candidate, trustedProxies) {
						return candidate.String()
					}
				}
			}
		}
	}

	// Default: use RemoteAddr in canonical form.
	if ip := net.ParseIP(remoteHost); ip != nil {
		return ip.String()
	}
	return remoteHost
}

func isTrustedProxy(ip net.IP, cidrs []*net.IPNet) bool {
	for _, cidr := range cidrs {
		if cidr.Contains(ip) {
			return true
		}
	}
	return false
}

func rateLimitKey(path, ip string) string {
	sanitized := strings.TrimPrefix(path, "/")
	sanitized = strings.ReplaceAll(sanitized, "/", ":")
	return fmt.Sprintf("mul:ratelimit:%s:%s", sanitized, ip)
}
