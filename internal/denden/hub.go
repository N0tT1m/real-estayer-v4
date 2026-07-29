package denden

// hub.go adds an OPTIONAL HTTP shipper to the denden shim, for apps that the
// docker_logs forwarder can't see — chiefly the nami-agent, which runs as a
// plain native binary on a remote Windows host with the media mounted.
//
// When $DENDEN_HUB_HTTP is set to the hub's http_server URL (e.g.
// http://REMOTE_HOST_REMOVED:9000), every log line is ALSO POSTed there as
// newline-delimited JSON — the exact framing den-den-mushi's `http` source
// expects (framing: newline_delimited, decoding: bytes), so lines land in
// ClickHouse identically to docker/file sources. Empty (the default) means
// stdout only, exactly as before — zero overhead, no network dependency.
//
// Everything here is pure net/http, so it behaves identically on
// Windows/Linux/macOS — no shell, no vendor script, no platform build tags.

import (
	"bytes"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// HubEnv names the env var that turns on HTTP shipping to the hub.
const HubEnv = "DENDEN_HUB_HTTP"

// HubEnvAliases are accepted as fallbacks for HubEnv.
//
// The fleet independently grew three names for the same setting: this shim and
// nami use DENDEN_HUB_HTTP, uta-foundry's ship.go reads DENDEN_HTTP_URL, and
// the hand-rolled shippers in frost-tit-thermals and monet-snowfall read
// DENDEN_HUB_URL. Setting one did nothing for services expecting another,
// which is a large part of why platform-wide logging never actually landed.
//
// Rather than force every deployment to change at once, resolve all three in
// priority order. New deployments should set DENDEN_HUB_HTTP.
var HubEnvAliases = []string{"DENDEN_HUB_URL", "DENDEN_HTTP_URL"}

// HubURL returns the configured hub endpoint, checking HubEnv first and then
// each alias. Empty means "no HTTP shipping" and the caller stays stdout-only.
func HubURL() string {
	for _, k := range append([]string{HubEnv}, HubEnvAliases...) {
		if v := strings.TrimSpace(os.Getenv(k)); v != "" {
			return v
		}
	}
	return ""
}

// HubUserEnv / HubPasswordEnv carry optional basic-auth credentials for hubs
// that have the http_server listener's auth block enabled. Unset is the normal
// case and sends no Authorization header at all.
const (
	HubUserEnv     = "DENDEN_HUB_USER"
	HubPasswordEnv = "DENDEN_HUB_PASSWORD"
)

func hubCredentials() (string, string) {
	return strings.TrimSpace(os.Getenv(HubUserEnv)), os.Getenv(HubPasswordEnv)
}

// hostName is stamped onto every line so the hub can tell which box a log came
// from — essential when several native agent hosts push over HTTP.
var hostName = func() string { h, _ := os.Hostname(); return h }()

// hubWriter wraps base (normally os.Stdout): if $DENDEN_HUB_HTTP is set it
// returns a tee that also ships each line to the hub; otherwise it returns base
// unchanged, so the default stdout-only path is untouched.
func hubWriter(base io.Writer) io.Writer {
	url := HubURL()
	if url == "" {
		return base
	}
	return &teeHub{base: base, sink: newHTTPSink(url)}
}

// teeHub writes to the base sink (stdout) and best-effort tees to the hub.
// The base write's result is what callers see — the hub is never allowed to
// fail or slow down logging.
type teeHub struct {
	base io.Writer
	sink *httpSink
}

func (t *teeHub) Write(p []byte) (int, error) {
	n, err := t.base.Write(p)
	t.sink.enqueue(p) // copies p; never blocks
	return n, err
}

// httpSink batches log lines and POSTs them to the hub on a background
// goroutine. Enqueue is non-blocking and drops on overload, so a slow or down
// hub can never stall the transcode agent — stdout still has every line.
type httpSink struct {
	url    string
	ch     chan []byte
	client *http.Client
}

func newHTTPSink(url string) *httpSink {
	s := &httpSink{
		url:    url,
		ch:     make(chan []byte, 4096),
		client: &http.Client{Timeout: 5 * time.Second},
	}
	go s.run()
	return s
}

func (s *httpSink) enqueue(p []byte) {
	// slog/logrus reuse their buffer after Write returns, so copy before
	// handing the bytes to another goroutine.
	b := make([]byte, len(p))
	copy(b, p)
	select {
	case s.ch <- b:
	default:
		// Buffer full (hub slow/unreachable) — drop rather than block the app.
		// Nothing is lost from the local view: stdout already has the line.
	}
}

// run coalesces lines and flushes every second or every maxBatch lines.
func (s *httpSink) run() {
	const maxBatch = 256
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	var batch bytes.Buffer
	lines := 0
	flush := func() {
		if lines == 0 {
			return
		}
		req, err := http.NewRequest(http.MethodPost, s.url, bytes.NewReader(batch.Bytes()))
		if err == nil {
			req.Header.Set("Content-Type", "application/x-ndjson")
			// Sent only when configured, so this is a no-op against a hub with
			// auth disabled (the default) and correct against one with it on.
			if user, pass := hubCredentials(); user != "" {
				req.SetBasicAuth(user, pass)
			}
			if resp, derr := s.client.Do(req); derr == nil {
				resp.Body.Close()
			}
			// On error we simply drop this batch — stdout remains the source of
			// truth and the hub catches up on the next line.
		}
		batch.Reset()
		lines = 0
	}

	for {
		select {
		case b := <-s.ch:
			batch.Write(b)
			if len(b) == 0 || b[len(b)-1] != '\n' {
				batch.WriteByte('\n') // newline_delimited framing
			}
			if lines++; lines >= maxBatch {
				flush()
			}
		case <-ticker.C:
			flush()
		}
	}
}
