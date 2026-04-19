package handler

import (
	"errors"
	"net/http"

	"github.com/realestayer/v4/internal/service"
)

// StartTOTP generates a fresh secret + provisioning URI for the logged-in
// user. The UI passes the URI to a QR renderer (we use a public image
// endpoint to avoid bundling a QR generator).
func (h *Handler) StartTOTP(w http.ResponseWriter, r *http.Request) {
	secret, uri, err := h.authService.StartTOTPEnrollment(r.Context(), h.getUserID(r), "Real-Estayer")
	if err != nil {
		h.jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.jsonResponse(w, http.StatusOK, map[string]string{
		"secret": secret,
		"uri":    uri,
	})
}

type totpConfirmReq struct {
	Code string `json:"code"`
}

// ConfirmTOTP turns the pending secret into an active factor.
func (h *Handler) ConfirmTOTP(w http.ResponseWriter, r *http.Request) {
	var req totpConfirmReq
	if err := h.parseJSON(r, &req); err != nil {
		h.jsonError(w, http.StatusBadRequest, "invalid body")
		return
	}
	if err := h.authService.ConfirmTOTP(r.Context(), h.getUserID(r), req.Code); err != nil {
		if errors.Is(err, service.ErrTOTPInvalid) {
			h.jsonError(w, http.StatusBadRequest, "that code didn't match — try again")
			return
		}
		h.jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.jsonResponse(w, http.StatusOK, map[string]string{"message": "two-factor enabled"})
}

// DisableTOTP removes the factor after the user proves they still have
// access to the enrolled device.
func (h *Handler) DisableTOTP(w http.ResponseWriter, r *http.Request) {
	var req totpConfirmReq
	if err := h.parseJSON(r, &req); err != nil {
		h.jsonError(w, http.StatusBadRequest, "invalid body")
		return
	}
	if err := h.authService.DisableTOTP(r.Context(), h.getUserID(r), req.Code); err != nil {
		if errors.Is(err, service.ErrTOTPInvalid) {
			h.jsonError(w, http.StatusBadRequest, "that code didn't match")
			return
		}
		h.jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.jsonResponse(w, http.StatusOK, map[string]string{"message": "two-factor disabled"})
}
