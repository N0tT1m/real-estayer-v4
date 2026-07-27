package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"go.mongodb.org/mongo-driver/bson/primitive"

	"github.com/realestayer/v4/internal/models"
)

// withUser returns a request carrying an authenticated user, the way
// RequireAuth leaves it for downstream middleware.
func withUser(t *testing.T, remoteAddr string, userID primitive.ObjectID) *http.Request {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/ai/itinerary", nil)
	req.RemoteAddr = remoteAddr
	ctx := context.WithValue(req.Context(), UserContextKey, &models.User{ID: userID})
	return req.WithContext(ctx)
}

// The point of per-account keying: two people behind one NAT must not share a
// budget. Under KeyByIP the second user would inherit the first's exhausted
// bucket.
func TestRateLimitKeyedSeparatesUsersOnOneIP(t *testing.T) {
	h := RateLimitKeyed(60, 1, KeyByUserOrIP)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	alice := primitive.NewObjectID()
	bob := primitive.NewObjectID()
	const sharedIP = "203.0.113.7:5555"

	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, withUser(t, sharedIP, alice))
	if rr.Code != http.StatusOK {
		t.Fatalf("alice's first request should pass; got %d", rr.Code)
	}

	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, withUser(t, sharedIP, alice))
	if rr.Code != http.StatusTooManyRequests {
		t.Fatalf("alice's second request should throttle; got %d", rr.Code)
	}

	// Same IP, different account — must get its own bucket.
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, withUser(t, sharedIP, bob))
	if rr.Code != http.StatusOK {
		t.Fatalf("bob should not inherit alice's quota; got %d", rr.Code)
	}
}

// The other direction: one account must not multiply its quota by moving
// addresses, which per-IP keying would allow.
func TestRateLimitKeyedFollowsUserAcrossIPs(t *testing.T) {
	h := RateLimitKeyed(60, 1, KeyByUserOrIP)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	alice := primitive.NewObjectID()

	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, withUser(t, "198.51.100.1:1111", alice))
	if rr.Code != http.StatusOK {
		t.Fatalf("first request should pass; got %d", rr.Code)
	}

	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, withUser(t, "198.51.100.99:2222", alice))
	if rr.Code != http.StatusTooManyRequests {
		t.Fatalf("rotating IP should not refill the bucket; got %d", rr.Code)
	}
}

// Unauthenticated callers still fall back to IP buckets.
func TestKeyByUserOrIPFallsBackToIP(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "192.0.2.5:9999"

	if got, want := KeyByUserOrIP(req), "ip:192.0.2.5"; got != want {
		t.Fatalf("anonymous key = %q, want %q", got, want)
	}

	id := primitive.NewObjectID()
	if got, want := KeyByUserOrIP(withUser(t, "192.0.2.5:9999", id)), "u:"+id.Hex(); got != want {
		t.Fatalf("authenticated key = %q, want %q", got, want)
	}
}
