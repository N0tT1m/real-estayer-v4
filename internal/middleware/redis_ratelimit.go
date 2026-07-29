package middleware

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// RateLimiter is the contract both the in-memory and Redis limiters satisfy.
// Exposed so main.go can pick one based on config without branching at the
// route level.
type RateLimiter interface {
	Middleware() func(http.Handler) http.Handler
}

// InMemoryRateLimiter wraps the existing per-process limiter behind the
// interface.
type InMemoryRateLimiter struct {
	rate, burst int
	key         KeyFunc
}

// NewInMemoryRateLimiter keeps the original `RateLimit` call site working —
// same constructor signature, different type.
func NewInMemoryRateLimiter(ratePerMinute, burst int) *InMemoryRateLimiter {
	return &InMemoryRateLimiter{rate: ratePerMinute, burst: burst, key: KeyByIP}
}

func (l *InMemoryRateLimiter) Middleware() func(http.Handler) http.Handler {
	return RateLimitKeyed(l.rate, l.burst, l.key)
}

// NewRateLimiter picks the Redis-backed implementation when redisURL is
// non-empty, otherwise falls back to in-memory. Bucket key prefixes let
// multiple limiters share a single Redis without colliding — pass something
// like "auth" or "api" per mount.
func NewRateLimiter(redisURL, keyPrefix string, ratePerMinute, burst int) RateLimiter {
	return NewRateLimiterKeyed(redisURL, keyPrefix, ratePerMinute, burst, KeyByIP)
}

// NewRateLimiterKeyed is NewRateLimiter with a caller-chosen bucket key, for
// mounts where per-IP accounting is the wrong unit. Note that a key func
// reading request context (KeyByUserOrIP) only sees what earlier middleware
// put there, so mount it inside the authenticated group, not above it.
func NewRateLimiterKeyed(redisURL, keyPrefix string, ratePerMinute, burst int, key KeyFunc) RateLimiter {
	if key == nil {
		key = KeyByIP
	}
	if redisURL == "" {
		return &InMemoryRateLimiter{rate: ratePerMinute, burst: burst, key: key}
	}
	l := NewRedisRateLimiter(redisURL, keyPrefix, ratePerMinute, burst)
	l.key = key
	l.fallback.key = key
	return l
}

// RedisRateLimiter implements the token-bucket algorithm using Redis' INCR
// + EXPIRE. All shared state lives in Redis so multiple app replicas behind
// a load balancer agree on limits.
//
// We use a minimal RESP2 client (dependency-free) rather than pulling the
// full go-redis module — INCR and EXPIRE are the only commands we need.
type RedisRateLimiter struct {
	client   *redisClient
	prefix   string
	rate     int // per minute
	burst    int
	key      KeyFunc
	fallback *InMemoryRateLimiter // used when Redis is unreachable

	// downMu guards downUntil only. It is never held across a Redis call.
	downMu    sync.Mutex
	downUntil time.Time
}

// redisDownCooldown is how long a Redis failure diverts traffic straight to the
// in-memory limiter before we probe again.
//
// Per-command deadlines bound one request, but not the system: incrWithExpire
// serialises on the client mutex, so with a wedged Redis every request would
// still pay the timeout in turn and throughput would collapse to roughly one
// request per timeout across the whole limiter — requests queueing on the lock
// rather than being served. The breaker means a single probe absorbs that cost
// per window and everyone else takes the fallback immediately.
const redisDownCooldown = 5 * time.Second

// tripBreaker diverts subsequent requests to the fallback for the cooldown.
func (l *RedisRateLimiter) tripBreaker() {
	l.downMu.Lock()
	l.downUntil = time.Now().Add(redisDownCooldown)
	l.downMu.Unlock()
}

// breakerOpen reports whether Redis is being skipped right now.
func (l *RedisRateLimiter) breakerOpen() bool {
	l.downMu.Lock()
	defer l.downMu.Unlock()
	return time.Now().Before(l.downUntil)
}

func NewRedisRateLimiter(redisURL, prefix string, ratePerMinute, burst int) *RedisRateLimiter {
	c, err := dialRedis(redisURL)
	if err != nil {
		slog.Warn("rate limiter: redis dial failed, falling back to in-memory", "error", err)
	}
	return &RedisRateLimiter{
		client:   c,
		prefix:   prefix,
		rate:     ratePerMinute,
		burst:    burst,
		key:      KeyByIP,
		fallback: NewInMemoryRateLimiter(ratePerMinute, burst),
	}
}

func (l *RedisRateLimiter) Middleware() func(http.Handler) http.Handler {
	if l.client == nil {
		return l.fallback.Middleware()
	}
	window := 60 // seconds
	fallback := l.fallback.Middleware()
	return func(next http.Handler) http.Handler {
		// Wrap `next` once with the in-memory limiter so we can hand any
		// Redis-faulted request to a real limiter instead of letting it
		// through unprotected. This preserves protection on auth endpoints
		// during a Redis outage at the cost of per-replica (rather than
		// global) accounting.
		fallbackHandler := fallback(next)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Skip Redis entirely while the breaker is open, so a wedged
			// instance costs one probe per cooldown rather than one timeout
			// per request.
			if l.breakerOpen() {
				fallbackHandler.ServeHTTP(w, r)
				return
			}
			key := "rl:" + l.prefix + ":" + l.key(r)
			count, err := l.client.incrWithExpire(key, window)
			if err != nil {
				l.tripBreaker()
				slog.Warn("rate limiter: redis error, falling back to in-memory",
					"error", err, "cooldown", redisDownCooldown)
				fallbackHandler.ServeHTTP(w, r)
				return
			}
			// Allow up to (rate + burst) within the window.
			if count > int64(l.rate+l.burst) {
				w.Header().Set("Retry-After", strconv.Itoa(window))
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

// --- Minimal RESP2 client ---

type redisClient struct {
	addr     string
	password string
	db       int

	mu   sync.Mutex
	conn net.Conn
	rw   *bufio.ReadWriter
}

func dialRedis(raw string) (*redisClient, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, err
	}
	if u.Scheme != "redis" && u.Scheme != "rediss" {
		return nil, fmt.Errorf("unsupported redis scheme: %s", u.Scheme)
	}
	c := &redisClient{addr: u.Host}
	if pw, ok := u.User.Password(); ok {
		c.password = pw
	}
	if u.Path != "" && u.Path != "/" {
		if n, err := strconv.Atoi(strings.TrimPrefix(u.Path, "/")); err == nil {
			c.db = n
		}
	}
	if err := c.dial(); err != nil {
		return nil, err
	}
	return c, nil
}

// redisOpTimeout bounds a single command's write+read. Without it a Redis that
// accepts the connection and then stops answering (partition, swap, a blocking
// command on the server) parks the caller in net.Conn.Read forever — and since
// incrWithExpire holds c.mu for the whole round-trip, every later request on
// this limiter queues behind it permanently. The router's 60s Timeout does not
// save us: it answers the client but leaves the goroutine blocked holding the
// lock. This sits in front of login/register/reset, so "fails closed and slow"
// has to mean "gives up and falls back", not "hangs".
const redisOpTimeout = 2 * time.Second

func (c *redisClient) dial() error {
	// Close the connection being replaced. Reconnect is the error path, so
	// leaking here means leaking a descriptor per Redis blip.
	if c.conn != nil {
		_ = c.conn.Close()
		c.conn = nil
		c.rw = nil
	}
	conn, err := net.DialTimeout("tcp", c.addr, 3*time.Second)
	if err != nil {
		return err
	}
	c.conn = conn
	c.rw = bufio.NewReadWriter(bufio.NewReader(conn), bufio.NewWriter(conn))
	if c.password != "" {
		if _, err := c.cmd("AUTH", c.password); err != nil {
			return err
		}
	}
	if c.db != 0 {
		if _, err := c.cmd("SELECT", strconv.Itoa(c.db)); err != nil {
			return err
		}
	}
	return nil
}

// incrWithExpire atomically INCRs a key and sets a TTL on first increment.
// A small script would do this in one round-trip; we use two for simplicity.
func (c *redisClient) incrWithExpire(key string, ttlSeconds int) (int64, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	reply, err := c.cmd("INCR", key)
	if err != nil {
		// Reconnect once and retry — handles Redis restarts.
		if err2 := c.dial(); err2 == nil {
			reply, err = c.cmd("INCR", key)
		}
		if err != nil {
			return 0, err
		}
	}
	count, _ := reply.(int64)
	// Only set the TTL on the first hit so the window stays aligned.
	if count == 1 {
		_, _ = c.cmd("EXPIRE", key, strconv.Itoa(ttlSeconds))
	}
	return count, nil
}

// redisServerError is a well-formed "-ERR ..." reply. The stream stays in sync
// after one, so it must not tear the connection down — unlike an I/O failure,
// which leaves an unknown number of bytes pending.
type redisServerError struct{ msg string }

func (e *redisServerError) Error() string { return "redis: " + e.msg }

// invalidate drops the connection so the next call redials. Used after any I/O
// error, including a deadline expiry: a command that timed out mid-flight may
// still have a reply in transit, and reading it as the answer to the *next*
// command would silently corrupt every subsequent count.
func (c *redisClient) invalidate() {
	if c.conn != nil {
		_ = c.conn.Close()
	}
	c.conn = nil
	c.rw = nil
}

// cmd sends one command and reads one reply. Replies are int64, string,
// nil (interface{}(nil)), or error.
func (c *redisClient) cmd(args ...string) (any, error) {
	if c.conn == nil || c.rw == nil {
		return nil, errors.New("redis: not connected")
	}
	if err := c.conn.SetDeadline(time.Now().Add(redisOpTimeout)); err != nil {
		c.invalidate()
		return nil, err
	}
	// Wire up RESP2 inline array.
	_, _ = fmt.Fprintf(c.rw, "*%d\r\n", len(args))
	for _, a := range args {
		_, _ = fmt.Fprintf(c.rw, "$%d\r\n%s\r\n", len(a), a)
	}
	if err := c.rw.Flush(); err != nil {
		c.invalidate()
		return nil, err
	}
	reply, err := c.readReply()
	if err != nil {
		var serverErr *redisServerError
		if !errors.As(err, &serverErr) {
			c.invalidate()
		}
		return nil, err
	}
	return reply, nil
}

func (c *redisClient) readReply() (any, error) {
	line, err := c.rw.ReadString('\n')
	if err != nil {
		return nil, err
	}
	if len(line) < 2 {
		return nil, fmt.Errorf("short reply")
	}
	switch line[0] {
	case '+':
		return strings.TrimRight(line[1:], "\r\n"), nil
	case '-':
		return nil, &redisServerError{msg: strings.TrimRight(line[1:], "\r\n")}
	case ':':
		return strconv.ParseInt(strings.TrimRight(line[1:], "\r\n"), 10, 64)
	case '$':
		n, err := strconv.Atoi(strings.TrimRight(line[1:], "\r\n"))
		if err != nil {
			return nil, err
		}
		if n < 0 {
			return nil, nil
		}
		buf := make([]byte, n+2)
		if _, err := readFull(c.rw, buf); err != nil {
			return nil, err
		}
		return string(buf[:n]), nil
	}
	return nil, fmt.Errorf("unexpected reply type %q", line[0])
}

func readFull(r *bufio.ReadWriter, buf []byte) (int, error) {
	total := 0
	for total < len(buf) {
		n, err := r.Read(buf[total:])
		total += n
		if err != nil {
			return total, err
		}
	}
	return total, nil
}
