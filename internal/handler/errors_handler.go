package handler

import (
	"net/http"
	"strings"

	chimw "github.com/go-chi/chi/v5/middleware"
)

// NotFound renders the custom 404 page for HTML routes and a JSON body for
// API routes. Chi's default sends bare text/plain.
func (h *Handler) NotFound(w http.ResponseWriter, r *http.Request) {
	if wantsJSON(r) {
		h.jsonError(w, http.StatusNotFound, "not found")
		return
	}
	w.WriteHeader(http.StatusNotFound)
	h.render(w, r, "error_404.html", map[string]interface{}{
		"Title": "Page not found",
	})
}

// ServerError is the standard 500 renderer used by the Recoverer. It takes
// the chi request ID (stamped by the RequestID middleware) so the template
// can show a reference code.
func (h *Handler) ServerError(w http.ResponseWriter, r *http.Request) {
	reqID := chimw.GetReqID(r.Context())
	if wantsJSON(r) {
		h.jsonResponse(w, http.StatusInternalServerError, map[string]string{
			"error":      "internal server error",
			"request_id": reqID,
		})
		return
	}
	w.WriteHeader(http.StatusInternalServerError)
	h.render(w, r, "error_500.html", map[string]interface{}{
		"Title":     "Something broke",
		"RequestID": reqID,
	})
}

// wantsJSON returns true for /api/ paths or requests that Accept JSON. Keeps
// error responses consistent with the rest of the app.
func wantsJSON(r *http.Request) bool {
	if strings.HasPrefix(r.URL.Path, "/api/") {
		return true
	}
	accept := r.Header.Get("Accept")
	return strings.Contains(accept, "application/json") && !strings.Contains(accept, "text/html")
}
