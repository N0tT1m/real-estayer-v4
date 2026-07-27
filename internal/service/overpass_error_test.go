package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// newTestOverpass points the service at a stub instead of the public Overpass
// endpoint. The endpoint field is unexported, but these tests live in the same
// package, so no seam has to be added to production code.
func newTestOverpass(t *testing.T, h http.HandlerFunc) *OverpassService {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	s := NewOverpassService()
	s.endpoint = srv.URL
	return s
}

// A 429 or 504 from the public instance is not the caller's fault. Both used to
// reach the browser as HTTP 400, blaming the user for a condition they can only
// wait out.
func TestNearbyUpstreamBusyStatuses(t *testing.T) {
	for _, status := range []int{http.StatusTooManyRequests, http.StatusGatewayTimeout} {
		s := newTestOverpass(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(status)
		})
		_, err := s.Nearby(context.Background(), "food", 38.72, -9.14, 1000)
		if err == nil {
			t.Fatalf("status %d: expected an error", status)
		}
		if !errors.Is(err, ErrUpstreamBusy) {
			t.Errorf("status %d: error = %v; want it to wrap ErrUpstreamBusy", status, err)
		}
	}
}

// Anything else upstream is still a generic failure — not ErrUpstreamBusy,
// since retrying a 500 in 30 seconds is not obviously useful.
func TestNearbyOtherUpstreamStatusIsNotBusy(t *testing.T) {
	s := newTestOverpass(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	_, err := s.Nearby(context.Background(), "food", 38.72, -9.14, 1000)
	if err == nil {
		t.Fatal("expected an error")
	}
	if errors.Is(err, ErrUpstreamBusy) {
		t.Errorf("a 500 should not be reported as upstream-busy: %v", err)
	}
	if errors.Is(err, ErrUnsupportedCategory) {
		t.Errorf("a 500 is not a category problem: %v", err)
	}
}

func TestNearbyUnsupportedCategory(t *testing.T) {
	s := newTestOverpass(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("unsupported category should be rejected before any request is made")
	})
	_, err := s.Nearby(context.Background(), "spacerocket", 38.72, -9.14, 1000)
	if !errors.Is(err, ErrUnsupportedCategory) {
		t.Errorf("error = %v; want it to wrap ErrUnsupportedCategory", err)
	}
}

// An over-large radius used to fall all the way back to 1500 m, so asking for
// 12 km silently searched 1.5 km and looked like "nothing here". It must clamp
// to the ceiling instead.
func TestNearbyClampsAnOversizedRadius(t *testing.T) {
	var gotQuery string
	s := newTestOverpass(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		gotQuery = string(body)
		_, _ = w.Write([]byte(`{"elements":[]}`))
	})
	if _, err := s.Nearby(context.Background(), "food", 38.72, -9.14, 50000); err != nil {
		t.Fatalf("Nearby: %v", err)
	}
	// The query is form-encoded, so the comma in "around:10000," arrives as %2C.
	if !strings.Contains(gotQuery, "around%3A10000") {
		t.Errorf("radius was not clamped to %d m; query was:\n%s", maxRadiusM, gotQuery)
	}
	if strings.Contains(gotQuery, "around%3A1500") {
		t.Error("radius collapsed to the default instead of clamping to the ceiling")
	}
}

func TestNearbyNonPositiveRadiusUsesDefault(t *testing.T) {
	var gotQuery string
	s := newTestOverpass(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		gotQuery = string(body)
		_, _ = w.Write([]byte(`{"elements":[]}`))
	})
	if _, err := s.Nearby(context.Background(), "food", 38.72, -9.14, 0); err != nil {
		t.Fatalf("Nearby: %v", err)
	}
	if !strings.Contains(gotQuery, "around%3A1500") {
		t.Errorf("radius 0 should fall back to %d m; query was:\n%s", defaultRadiusM, gotQuery)
	}
}

// A radius inside the range must be passed through untouched — the clamp
// should not be quietly rewriting ordinary requests.
func TestNearbyKeepsAnInRangeRadius(t *testing.T) {
	var gotQuery string
	s := newTestOverpass(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		gotQuery = string(body)
		_, _ = w.Write([]byte(`{"elements":[]}`))
	})
	if _, err := s.Nearby(context.Background(), "food", 38.72, -9.14, 3000); err != nil {
		t.Fatalf("Nearby: %v", err)
	}
	if !strings.Contains(gotQuery, "around%3A3000") {
		t.Errorf("in-range radius was altered; query was:\n%s", gotQuery)
	}
}
