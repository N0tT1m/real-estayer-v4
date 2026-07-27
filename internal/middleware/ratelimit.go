package middleware

import (
	"encoding/json"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// KeyFunc derives the bucket a request counts against.
type KeyFunc func(*http.Request) string

// KeyByIP is the default: one bucket per client IP.
func KeyByIP(r *http.Request) string { return clientIP(r) }

// KeyByUserOrIP buckets authenticated callers by user ID and everyone else by
// IP. Use it on routes behind RequireAuth where the resource being protected
// is per-account — AI generation, paid upstream calls — rather than per-host.
// IP keying is wrong there in both directions: it throttles a whole NAT
// collectively, and it lets one account multiply its quota by rotating
// addresses. The prefixes keep a user ID from ever colliding with an IP.
func KeyByUserOrIP(r *http.Request) string {
	if id := GetUserID(r.Context()); id != "" {
		return "u:" + id
	}
	return "ip:" + clientIP(r)
}

// RateLimit is a simple token-bucket limiter keyed by client IP. Use it on
// sensitive endpoints like login/register. Rate is per minute; burst allows
// short spikes. The bucket map is cleaned up lazily.
func RateLimit(ratePerMinute, burst int) func(http.Handler) http.Handler {
	return RateLimitKeyed(ratePerMinute, burst, KeyByIP)
}

// RateLimitKeyed is RateLimit with a caller-chosen bucket key.
func RateLimitKeyed(ratePerMinute, burst int, key KeyFunc) func(http.Handler) http.Handler {
	if key == nil {
		key = KeyByIP
	}
	rl := &rateLimiter{
		buckets: make(map[string]*bucket),
		rate:    float64(ratePerMinute) / 60.0,
		burst:   float64(burst),
	}
	go rl.reap()
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !rl.allow(key(r)) {
				w.Header().Set("Retry-After", "60")
				if strings.HasPrefix(r.URL.Path, "/api/") {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusTooManyRequests)
					_ = json.NewEncoder(w).Encode(map[string]string{"error": "rate limit exceeded"})
				} else {
					http.Error(w, "Too Many Requests", http.StatusTooManyRequests)
				}
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

type bucket struct {
	tokens float64
	last   time.Time
}

type rateLimiter struct {
	mu      sync.Mutex
	buckets map[string]*bucket
	rate    float64 // tokens per second
	burst   float64
}

func (rl *rateLimiter) allow(key string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	b, ok := rl.buckets[key]
	if !ok {
		rl.buckets[key] = &bucket{tokens: rl.burst - 1, last: now}
		return true
	}

	elapsed := now.Sub(b.last).Seconds()
	b.tokens += elapsed * rl.rate
	if b.tokens > rl.burst {
		b.tokens = rl.burst
	}
	b.last = now

	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

func (rl *rateLimiter) reap() {
	for range time.Tick(5 * time.Minute) {
		rl.mu.Lock()
		cutoff := time.Now().Add(-10 * time.Minute)
		for k, b := range rl.buckets {
			if b.last.Before(cutoff) {
				delete(rl.buckets, k)
			}
		}
		rl.mu.Unlock()
	}
}

func clientIP(r *http.Request) string {
	// chi's middleware.RealIP sets r.RemoteAddr to the real client IP when a
	// trusted proxy header is present, so RemoteAddr is authoritative here.
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
