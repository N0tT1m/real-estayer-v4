// Package denden is the canonical structured logger for the megaverse.
//
// It wraps the stdlib log/slog with a JSON handler that emits the den-den-mushi
// schema ({ts, level, app, host, pid, msg, ...}) to stdout. Apps keep using
// slog's normal API; the collector tails stdout and ships it to ClickHouse.
//
//	log := denden.New("waifu-foundry")
//	log.Info("morph baked", "donor", "boa", "drift", 0.12)
//	log.Error("img2img failed", "err", err)
//
// Dockerized apps need nothing else — the docker_logs forwarder tails stdout.
// NATIVE apps that no forwarder watches (e.g. an agent on a remote Windows box)
// set $DENDEN_HUB_HTTP to the hub's http_server URL and the logger ALSO ships
// each line there over HTTP, batched and non-blocking (see hub.go). Either way
// stdout always gets every line, so there's no data loss if the hub is down.
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
