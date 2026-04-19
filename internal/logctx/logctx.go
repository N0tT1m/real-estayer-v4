// Package logctx pulls request-scoped fields off a context and binds them to
// slog.Logger so service-level log calls inherit the HTTP request_id they
// were triggered by. Services that accept ctx can call logctx.From(ctx) in
// place of slog.Default() and every line they emit gets the request_id
// without the service layer having to know about chi's middleware.
package logctx

import (
	"context"
	"log/slog"

	chimw "github.com/go-chi/chi/v5/middleware"
)

// From returns the default logger with request-scoped fields attached when
// present. Falls back to slog.Default() for background goroutines and tests
// that don't carry a request context.
func From(ctx context.Context) *slog.Logger {
	if ctx == nil {
		return slog.Default()
	}
	if id := chimw.GetReqID(ctx); id != "" {
		return slog.Default().With("request_id", id)
	}
	return slog.Default()
}
