package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestTransitFallbackWhenUnconfigured(t *testing.T) {
	// Stub OSRM so the fallback path has something to talk to.
	osrmSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"routes":[{"distance":1000,"duration":600,"geometry":{}}]}`))
	}))
	defer osrmSrv.Close()

	osrm := NewRoutingService()
	osrm.base = osrmSrv.URL
	osrm.client.Transport = rewriteTransport(osrmSrv.URL)

	t2 := NewTransitService("", osrm) // no Google key → fallback
	r, err := t2.Route(context.Background(), 0, 0, 1, 1, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if r.Mode != "driving" {
		t.Errorf("unconfigured transit should fall back to driving, got %s", r.Mode)
	}
	if r.Summary == "" {
		t.Errorf("fallback should include a disclaimer summary")
	}
}

func TestStripHTML(t *testing.T) {
	got := stripHTML(`Turn <b>left</b> onto <span style="x">High St</span>`)
	if got != "Turn left onto High St" {
		t.Errorf("stripHTML = %q", got)
	}
}
