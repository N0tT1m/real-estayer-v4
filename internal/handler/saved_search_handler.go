package handler

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/realestayer/v4/internal/models"
)

type createSavedSearchReq struct {
	Name          string                  `json:"name"`
	NotifyDiscord bool                    `json:"notify_discord"`
	Query         models.SavedSearchQuery `json:"query"`
}

func (h *Handler) ListSavedSearches(w http.ResponseWriter, r *http.Request) {
	items, err := h.savedSearchSvc.List(r.Context(), h.getUserID(r))
	if err != nil {
		h.jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.jsonResponse(w, http.StatusOK, map[string]interface{}{"saved_searches": items})
}

func (h *Handler) CreateSavedSearch(w http.ResponseWriter, r *http.Request) {
	var req createSavedSearchReq
	if err := h.parseJSON(r, &req); err != nil {
		h.jsonError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	ss, err := h.savedSearchSvc.Create(r.Context(), h.getUserID(r), req.Name, req.Query, req.NotifyDiscord)
	if err != nil {
		h.jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	h.jsonResponse(w, http.StatusCreated, ss)
}

func (h *Handler) DeleteSavedSearch(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := h.savedSearchSvc.Delete(r.Context(), h.getUserID(r), id); err != nil {
		h.jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	h.jsonResponse(w, http.StatusOK, map[string]string{"message": "deleted"})
}
