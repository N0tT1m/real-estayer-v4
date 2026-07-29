package middleware

import (
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// silentRedis accepts connections and never replies, standing in for a Redis
// that is reachable but wedged (network partition, swap, a blocking command).
func silentRedis(t *testing.T) net.Listener {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	go func() {
		var held []net.Conn
		for {
			conn, err := ln.Accept()
			if err != nil {
				for _, c := range held {
					_ = c.Close()
				}
				return
			}
			// Hold the connection open without writing anything.
			held = append(held, conn)
		}
	}()
	t.Cleanup(func() { _ = ln.Close() })
	return ln
}

// An unresponsive Redis must not park the request. Before redisOpTimeout
// existed this blocked in net.Conn.Read indefinitely while holding the
// client mutex, which wedged every later request on the same limiter —
// on /auth/login and /auth/reset among others.
func TestRedisRateLimiterFallsBackWhenRedisHangs(t *testing.T) {
	ln := silentRedis(t)

	rl := NewRedisRateLimiter("redis://"+ln.Addr().String(), "test", 10, 5)
	if rl.client == nil {
		t.Skip("dial did not complete; nothing to exercise")
	}

	reached := false
	h := rl.Middleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached = true
	}))

	done := make(chan struct{})
	go func() {
		defer close(done)
		h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/auth/login", nil))
	}()

	// Generous headroom over redisOpTimeout, still far below "hung forever".
	select {
	case <-done:
	case <-time.After(redisOpTimeout + 8*time.Second):
		t.Fatal("request blocked on an unresponsive redis instead of falling back")
	}

	if !reached {
		t.Error("request should have reached the handler via the in-memory fallback")
	}
}

// A hung command must not leave the request served by nothing: the fallback
// limiter still has to enforce a ceiling.
func TestRedisRateLimiterFallbackStillLimits(t *testing.T) {
	ln := silentRedis(t)

	rl := NewRedisRateLimiter("redis://"+ln.Addr().String(), "test", 2, 1)
	if rl.client == nil {
		t.Skip("dial did not complete; nothing to exercise")
	}
	h := rl.Middleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))

	limited := false
	for i := 0; i < 12; i++ {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/auth/login", nil)
		req.RemoteAddr = "203.0.113.9:1234"
		h.ServeHTTP(rec, req)
		if rec.Code == http.StatusTooManyRequests {
			limited = true
			break
		}
	}
	if !limited {
		t.Error("fallback limiter never returned 429; requests were effectively unlimited")
	}
}

// The per-command deadline bounds one request; the breaker is what keeps a
// wedged Redis from serialising every request behind a timeout. Without it,
// 5 requests would cost 5 timeouts back to back on the client mutex.
func TestRedisRateLimiterBreakerAvoidsPerRequestTimeouts(t *testing.T) {
	ln := silentRedis(t)

	rl := NewRedisRateLimiter("redis://"+ln.Addr().String(), "test", 100, 50)
	if rl.client == nil {
		t.Skip("dial did not complete; nothing to exercise")
	}
	h := rl.Middleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))

	serve := func() {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/auth/login", nil)
		req.RemoteAddr = "203.0.113.10:1234"
		h.ServeHTTP(rec, req)
	}

	serve() // first request pays the probe and trips the breaker

	// Subsequent requests inside the cooldown must not touch Redis at all.
	start := time.Now()
	for i := 0; i < 5; i++ {
		serve()
	}
	elapsed := time.Since(start)

	if elapsed > redisOpTimeout {
		t.Fatalf("5 requests took %v with the breaker open; expected near-instant "+
			"(a single redis timeout is %v)", elapsed, redisOpTimeout)
	}
}

// A dead connection must be replaced, not stacked on top of the previous one.
// dial() used to overwrite c.conn without closing it, leaking a descriptor per
// reconnect.
func TestRedisDialClosesReplacedConnection(t *testing.T) {
	ln := silentRedis(t)

	c := &redisClient{addr: ln.Addr().String()}
	if err := c.dial(); err != nil {
		t.Fatalf("first dial: %v", err)
	}
	first := c.conn
	if first == nil {
		t.Fatal("expected a connection after dial")
	}

	if err := c.dial(); err != nil {
		t.Fatalf("second dial: %v", err)
	}
	if c.conn == first {
		t.Fatal("redial reused the old connection")
	}
	// The replaced connection must already be closed.
	if err := first.SetDeadline(time.Now()); err == nil {
		t.Error("previous connection was left open by dial()")
	}
}

// A server-side "-ERR" reply is well-formed: the stream stays in sync, so the
// connection must survive it. Tearing down on every such reply would redial on
// routine errors.
func TestRedisServerErrorKeepsConnection(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = ln.Close() }()

	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		buf := make([]byte, 256)
		if _, err := conn.Read(buf); err != nil {
			return
		}
		_, _ = conn.Write([]byte("-ERR unknown command\r\n"))
	}()

	c := &redisClient{addr: ln.Addr().String()}
	if err := c.dial(); err != nil {
		t.Fatalf("dial: %v", err)
	}

	if _, err := c.cmd("INCR", "k"); err == nil {
		t.Fatal("expected the server error to surface")
	}
	if c.conn == nil {
		t.Error("connection was torn down by a well-formed server error reply")
	}
}
