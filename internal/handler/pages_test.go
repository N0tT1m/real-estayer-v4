package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/realestayer/v4/internal/database"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// Health used to write "OK" unconditionally, which made it useless as a probe:
// Docker's HEALTHCHECK and any load balancer in front would hold an instance in
// rotation with a dead database. These tests pin the part that matters — that
// it can actually fail.

func TestHealthReportsOKWhenDatabaseReachable(t *testing.T) {
	// A nil DB is the "nothing to check" case and must not be treated as a
	// failure; the handler skips the ping and reports ok.
	h := &Handler{}
	rec := httptest.NewRecorder()
	h.Health(rec, httptest.NewRequest(http.MethodGet, "/health", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("response is not JSON: %v (%s)", err, rec.Body.String())
	}
	if body["status"] != "ok" {
		t.Errorf("status field = %q, want ok", body["status"])
	}
}

func TestHealthReports503WhenDatabaseUnreachable(t *testing.T) {
	// Port 1 is reserved and never listening, so server selection fails. The
	// short selection timeout keeps this well inside the handler's own 2s
	// bound — otherwise the test would be measuring the driver, not us.
	client, err := mongo.Connect(context.Background(), options.Client().
		ApplyURI("mongodb://127.0.0.1:1/health-probe-test").
		SetServerSelectionTimeout(500*time.Millisecond))
	if err != nil {
		t.Fatalf("building an unreachable client should still succeed: %v", err)
	}
	defer func() { _ = client.Disconnect(context.Background()) }()

	h := &Handler{HandlerDeps: HandlerDeps{Core: CoreDeps{DB: &database.DB{Client: client}}}}
	rec := httptest.NewRecorder()
	h.Health(rec, httptest.NewRequest(http.MethodGet, "/health", nil))

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d — a probe that cannot fail is not a probe",
			rec.Code, http.StatusServiceUnavailable)
	}
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("response is not JSON: %v (%s)", err, rec.Body.String())
	}
	if body["status"] != "degraded" {
		t.Errorf("status field = %q, want degraded", body["status"])
	}
	if body["error"] == "" {
		t.Error("degraded response should name the failing subsystem")
	}
}

// The handler bounds its own ping at 2s so a hung database cannot hold the
// connection open until the 15s WriteTimeout. Verify it returns well before
// that even when the driver would happily keep waiting.
func TestHealthDoesNotHangOnADeadDatabase(t *testing.T) {
	client, err := mongo.Connect(context.Background(), options.Client().
		ApplyURI("mongodb://127.0.0.1:1/health-probe-test").
		SetServerSelectionTimeout(30*time.Second))
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer func() { _ = client.Disconnect(context.Background()) }()

	h := &Handler{HandlerDeps: HandlerDeps{Core: CoreDeps{DB: &database.DB{Client: client}}}}
	rec := httptest.NewRecorder()

	start := time.Now()
	h.Health(rec, httptest.NewRequest(http.MethodGet, "/health", nil))
	elapsed := time.Since(start)

	if elapsed > 5*time.Second {
		t.Errorf("Health took %v; the 2s internal timeout is not bounding it", elapsed)
	}
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusServiceUnavailable)
	}
}
