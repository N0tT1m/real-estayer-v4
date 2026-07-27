package middleware

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// A handler that outlives http.Server.WriteTimeout has its response discarded
// and the connection dropped — the client sees an empty reply rather than any
// status code. This is the failure ExtendWriteDeadline exists to prevent, so
// pin both halves against a real server: without the middleware the response
// is lost, with it the same slow handler answers normally.
func TestExtendWriteDeadlineDeliversSlowResponse(t *testing.T) {
	const (
		writeTimeout = 300 * time.Millisecond
		handlerTime  = 900 * time.Millisecond
	)

	slow := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(handlerTime)
		_, _ = io.WriteString(w, "generated")
	})

	t.Run("without middleware the response is dropped", func(t *testing.T) {
		body, err := getFrom(t, slow, writeTimeout)
		if err == nil {
			t.Fatalf("expected the write deadline to drop the response, got %q", body)
		}
	})

	t.Run("with middleware the response arrives", func(t *testing.T) {
		wrapped := ExtendWriteDeadline(5 * time.Second)(slow)
		body, err := getFrom(t, wrapped, writeTimeout)
		if err != nil {
			t.Fatalf("slow response should survive an extended deadline: %v", err)
		}
		if body != "generated" {
			t.Fatalf("body = %q, want %q", body, "generated")
		}
	})
}

// The middleware must not fail requests when the ResponseWriter has no
// deadline support — httptest's recorder is exactly that case, and so are some
// HTTP/2 paths in production.
func TestExtendWriteDeadlineToleratesUnsupportedWriter(t *testing.T) {
	called := false
	h := ExtendWriteDeadline(time.Minute)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))

	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/", nil))

	if !called {
		t.Fatal("handler should run even when the writer has no deadline support")
	}
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusOK)
	}
}

// getFrom serves h behind a server configured like cmd/server/main.go and
// returns the body, or an error if the response never arrived.
func getFrom(t *testing.T, h http.Handler, writeTimeout time.Duration) (string, error) {
	t.Helper()

	srv := httptest.NewUnstartedServer(h)
	srv.Config.WriteTimeout = writeTimeout
	srv.Start()
	defer srv.Close()

	resp, err := srv.Client().Get(srv.URL)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	return string(body), nil
}
