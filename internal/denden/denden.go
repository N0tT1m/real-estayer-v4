// Package denden is the structured logger used across this project.
//
// It wraps the stdlib log/slog with a JSON handler that emits a fixed envelope
// ({ts, level, app, host, pid, msg, ...}) to stdout. Callers keep using slog's
// normal API; a log collector tails stdout and ships it onward to a log store.
//
//	log := denden.New("real-estayer")
//	log.Info("listing scraped", "city", "lisbon", "results", 42)
//	log.Error("enrichment failed", "err", err)
//
// Containerized deployments need nothing else — the container log forwarder
// tails stdout. A process that no forwarder watches (for example the scraper
// running as a native binary on a separate host) sets $DENDEN_HUB_HTTP to a
// log hub's ingest URL, and the logger ALSO ships each line there over HTTP,
// batched and non-blocking (see hub.go). Either way stdout always gets every
// line, so nothing is lost when the hub is unreachable.
package denden

import (
	"io"
	"log/slog"
	"os"
	"strings"
)

// New returns a logger tagged with app. Level comes from $DENDEN_LEVEL
// (or $LOG_LEVEL), default info. Output goes to stdout, and — if
// $DENDEN_HUB_HTTP is set — is also shipped to the hub over HTTP (see hub.go).
func New(app string) *slog.Logger {
	return NewWriter(app, hubWriter(os.Stdout))
}

// NewWriter is New with an explicit sink. Production uses os.Stdout (via New);
// tests pass a buffer to assert on the emitted envelope. Lines are stamped with
// app, host, and pid so they're attributable in the hub.
func NewWriter(app string, w io.Writer) *slog.Logger {
	h := slog.NewJSONHandler(w, &slog.HandlerOptions{
		Level:       levelFromEnv(),
		ReplaceAttr: rename,
	})
	return slog.New(h).With("app", app, "host", hostName, "pid", os.Getpid())
}

// SetDefault installs New(app) as slog's package-level default, so existing
// slog.Info(...) calls across an app pick up the schema with no other change.
func SetDefault(app string) *slog.Logger {
	l := New(app)
	slog.SetDefault(l)
	return l
}

func rename(_ []string, a slog.Attr) slog.Attr {
	switch a.Key {
	case slog.TimeKey:
		a.Key = "ts"
		a.Value = slog.StringValue(a.Value.Time().UTC().Format("2006-01-02T15:04:05.000Z07:00"))
	case slog.LevelKey:
		a.Value = slog.StringValue(strings.ToLower(a.Value.String()))
	case slog.MessageKey:
		a.Key = "msg"
	}
	return a
}

func levelFromEnv() slog.Level {
	v := os.Getenv("DENDEN_LEVEL")
	if v == "" {
		v = os.Getenv("LOG_LEVEL")
	}
	switch strings.ToLower(v) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
