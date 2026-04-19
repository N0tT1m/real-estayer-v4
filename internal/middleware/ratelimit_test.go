package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRateLimitAllowsBurst(t *testing.T) {
	h := RateLimit(60, 3)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	for i := 0; i < 3; i++ {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = "10.0.0.1:1234"
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("request %d should be allowed in burst; got %d", i+1, rr.Code)
		}
	}
}

func TestRateLimitBlocksWhenBurstExhausted(t *testing.T) {
	// 60/min => 1 token/sec refill; burst 2 → 3rd request within <1s fails.
	h := RateLimit(60, 2)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = "10.0.0.2:1234"
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("request %d should be allowed; got %d", i+1, rr.Code)
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "10.0.0.2:1234"
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusTooManyRequests {
		t.Fatalf("third request should be throttled; got %d", rr.Code)
	}
	if rr.Header().Get("Retry-After") == "" {
		t.Errorf("expected Retry-After header on 429")
	}
}

func TestRateLimitIsPerIP(t *testing.T) {
	// Exhausting one IP's quota must not affect another IP.
	h := RateLimit(60, 1)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req1 := httptest.NewRequest(http.MethodGet, "/", nil)
	req1.RemoteAddr = "10.0.0.10:1"
	rr1 := httptest.NewRecorder()
	h.ServeHTTP(rr1, req1)
	if rr1.Code != http.StatusOK {
		t.Fatalf("IP A first request should pass; got %d", rr1.Code)
	}

	// IP A: second request should block.
	req1b := httptest.NewRequest(http.MethodGet, "/", nil)
	req1b.RemoteAddr = "10.0.0.10:1"
	rr1b := httptest.NewRecorder()
	h.ServeHTTP(rr1b, req1b)
	if rr1b.Code != http.StatusTooManyRequests {
		t.Fatalf("IP A second request should throttle; got %d", rr1b.Code)
	}

	// IP B: independent bucket, first request must pass.
	req2 := httptest.NewRequest(http.MethodGet, "/", nil)
	req2.RemoteAddr = "10.0.0.11:1"
	rr2 := httptest.NewRecorder()
	h.ServeHTTP(rr2, req2)
	if rr2.Code != http.StatusOK {
		t.Fatalf("IP B should not be affected by IP A's quota; got %d", rr2.Code)
	}
}
