package middleware

import (
	"log/slog"
	"net/http"
	"runtime/debug"

	chimw "github.com/go-chi/chi/v5/middleware"
)

// Recoverer recovers from panics, logs them with the request ID, fires the
// configured error hook (for pluggable Sentry/Rollbar integration), and
// delegates to `render500` so a styled page or JSON body is emitted.
//
// Unlike chi's built-in Recoverer this one doesn't write a generic
// text/plain response — the caller picks how to render.
func Recoverer(render500 http.HandlerFunc, hook func(req *http.Request, panicValue any, stack []byte)) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					if rec == http.ErrAbortHandler {
						// The stdlib convention: re-panic to signal deliberate abort.
						panic(rec)
					}
					stack := debug.Stack()
					slog.Error("request panic",
						"request_id", chimw.GetReqID(r.Context()),
						"method", r.Method,
						"path", r.URL.Path,
						"panic", rec,
						"stack", string(stack),
					)
					if hook != nil {
						hook(r, rec, stack)
					}
					render500(w, r)
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}
