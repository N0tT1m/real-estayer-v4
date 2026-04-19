package middleware

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	chimw "github.com/go-chi/chi/v5/middleware"
)

// RequestLogger logs one structured line per request. Replaces chi's own
// Logger so we get request_id plumbed into slog and no ANSI colour chrome.
func RequestLogger() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			ww := chimw.NewWrapResponseWriter(w, r.ProtoMajor)
			next.ServeHTTP(ww, r)

			attrs := []any{
				"request_id", chimw.GetReqID(r.Context()),
				"method", r.Method,
				"path", r.URL.Path,
				"status", ww.Status(),
				"bytes", ww.BytesWritten(),
				"duration_ms", float64(time.Since(start).Microseconds()) / 1000.0,
			}
			if ua := r.UserAgent(); ua != "" {
				if len(ua) > 120 {
					ua = ua[:117] + "..."
				}
				attrs = append(attrs, "ua", ua)
			}
			switch {
			case ww.Status() >= 500:
				slog.Error("http request", attrs...)
			case ww.Status() >= 400:
				slog.Warn("http request", attrs...)
			default:
				slog.Info("http request", attrs...)
			}
		})
	}
}

// Metrics is a dependency-free Prometheus-compatible counter + histogram
// middleware. Good enough for a single-binary deployment; swap for the
// official Prometheus client once we shard across replicas.
type Metrics struct {
	started   time.Time
	requests  sync.Map // key "<method> <status>" → *atomic.Uint64
	durations sync.Map // key "<method>" → *bucketedHistogram
}

type bucketedHistogram struct {
	bounds []float64 // in ms
	counts []atomic.Uint64
	sum    atomic.Uint64 // microseconds; keeps math integer-safe
	total  atomic.Uint64
}

var defaultBuckets = []float64{5, 10, 25, 50, 100, 250, 500, 1000, 2500, 5000, 10000}

func NewMetrics() *Metrics { return &Metrics{started: time.Now()} }

// Middleware returns the chi-compatible handler that counts every request.
func (m *Metrics) Middleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			ww := chimw.NewWrapResponseWriter(w, r.ProtoMajor)
			next.ServeHTTP(ww, r)

			key := r.Method + " " + strconv.Itoa(ww.Status())
			counter, _ := m.requests.LoadOrStore(key, &atomic.Uint64{})
			counter.(*atomic.Uint64).Add(1)

			histIface, _ := m.durations.LoadOrStore(r.Method, newHistogram(defaultBuckets))
			histIface.(*bucketedHistogram).observe(float64(time.Since(start).Microseconds()) / 1000.0)
		})
	}
}

func newHistogram(bounds []float64) *bucketedHistogram {
	return &bucketedHistogram{bounds: bounds, counts: make([]atomic.Uint64, len(bounds)+1)}
}

func (h *bucketedHistogram) observe(valueMs float64) {
	h.total.Add(1)
	h.sum.Add(uint64(valueMs * 1000))
	for i, b := range h.bounds {
		if valueMs <= b {
			h.counts[i].Add(1)
			return
		}
	}
	h.counts[len(h.counts)-1].Add(1)
}

// MetricsHandler renders /metrics in Prometheus exposition format.
func (m *Metrics) MetricsHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")

		fmt.Fprintln(w, "# HELP app_uptime_seconds Seconds since the process started.")
		fmt.Fprintln(w, "# TYPE app_uptime_seconds counter")
		fmt.Fprintf(w, "app_uptime_seconds %d\n", int64(time.Since(m.started).Seconds()))

		fmt.Fprintln(w, "# HELP http_requests_total Total HTTP requests by method and status.")
		fmt.Fprintln(w, "# TYPE http_requests_total counter")
		m.requests.Range(func(k, v any) bool {
			parts := splitOnce(k.(string), ' ')
			fmt.Fprintf(w, "http_requests_total{method=%q,status=%q} %d\n", parts[0], parts[1], v.(*atomic.Uint64).Load())
			return true
		})

		fmt.Fprintln(w, "# HELP http_request_duration_ms Request latency in milliseconds.")
		fmt.Fprintln(w, "# TYPE http_request_duration_ms histogram")
		m.durations.Range(func(k, v any) bool {
			method := k.(string)
			h := v.(*bucketedHistogram)
			cumulative := uint64(0)
			for i, b := range h.bounds {
				cumulative += h.counts[i].Load()
				fmt.Fprintf(w, "http_request_duration_ms_bucket{method=%q,le=%q} %d\n", method, strconv.FormatFloat(b, 'g', -1, 64), cumulative)
			}
			cumulative += h.counts[len(h.counts)-1].Load()
			fmt.Fprintf(w, "http_request_duration_ms_bucket{method=%q,le=\"+Inf\"} %d\n", method, cumulative)
			fmt.Fprintf(w, "http_request_duration_ms_sum{method=%q} %d\n", method, h.sum.Load()/1000)
			fmt.Fprintf(w, "http_request_duration_ms_count{method=%q} %d\n", method, h.total.Load())
			return true
		})
	}
}

func splitOnce(s string, sep byte) [2]string {
	for i := 0; i < len(s); i++ {
		if s[i] == sep {
			return [2]string{s[:i], s[i+1:]}
		}
	}
	return [2]string{s, ""}
}

// ---- Pluggable error hook ----

// ErrorHook is the pluggable contract the Recoverer middleware calls after
// a panic. Supply your own implementation wired to Sentry / Honeybadger /
// Rollbar, or use WebhookErrorHook to POST to a simple HTTP endpoint (handy
// for Mattermost/Slack/Discord incoming webhooks).
type ErrorHook func(req *http.Request, panicValue any, stack []byte)

// NoopErrorHook does nothing; the default when no webhook is configured.
func NoopErrorHook() ErrorHook {
	return func(*http.Request, any, []byte) {}
}

// WebhookErrorHook posts a compact JSON payload to the given URL when a
// panic is recovered. Fire-and-forget — errors from the hook itself are
// dropped so crash-reporting never blocks the response path.
func WebhookErrorHook(url string, client *http.Client) ErrorHook {
	if url == "" {
		return NoopErrorHook()
	}
	if client == nil {
		client = &http.Client{Timeout: 3 * time.Second}
	}
	return func(r *http.Request, panicValue any, stack []byte) {
		body, _ := json.Marshal(map[string]any{
			"request_id": chimw.GetReqID(r.Context()),
			"method":     r.Method,
			"path":       r.URL.Path,
			"panic":      fmt.Sprintf("%v", panicValue),
			"stack":      truncateStack(stack),
		})
		req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, url, bytes.NewReader(body))
		if err != nil {
			return
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := client.Do(req)
		if err != nil {
			return
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}
}

func truncateStack(b []byte) string {
	if len(b) > 1800 {
		return string(b[:1800]) + "\n...(truncated)"
	}
	return string(b)
}
