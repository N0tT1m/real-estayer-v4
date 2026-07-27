package middleware

import (
	"log/slog"
	"net/http"
	"time"
)

// ExtendWriteDeadline pushes the connection's write deadline out for handlers
// that legitimately run long.
//
// http.Server.WriteTimeout (set in cmd/server/main.go) is a hard deadline
// measured from the end of the request header read, so it bounds total handler
// time rather than just the write syscall. A handler that outlives it has its
// response discarded and the connection dropped — the client sees an empty
// reply, not a 504, and the server logs nothing. AI generation against a local
// model measured 17-45s in practice, well past the 15s server default, so
// every one of those responses would be thrown away.
//
// This moves only the per-connection deadline for the routes it wraps; every
// other route keeps the server default. Pass a duration above the request
// timeout ceiling (see RequestTimeout wiring in main.go) so a 504 from the
// timeout middleware can still be written to the wire.
func ExtendWriteDeadline(d time.Duration) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Not every ResponseWriter supports deadlines (httptest recorders
			// and some HTTP/2 paths don't). The handler is still correct
			// without it — it just keeps the server-wide WriteTimeout — so
			// log at debug rather than failing the request.
			if err := http.NewResponseController(w).SetWriteDeadline(time.Now().Add(d)); err != nil {
				slog.Debug("could not extend write deadline",
					"path", r.URL.Path, "error", err)
			}
			next.ServeHTTP(w, r)
		})
	}
}
