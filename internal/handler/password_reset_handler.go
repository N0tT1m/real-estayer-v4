package handler

import (
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/realestayer/v4/internal/service"
)

// ForgotPasswordPage renders the email-entry form.
func (h *Handler) ForgotPasswordPage(w http.ResponseWriter, r *http.Request) {
	h.render(w, r, "forgot_password.html", map[string]interface{}{
		"Title": "Forgot password",
	})
}

// RequestPasswordReset handles the email submission. Always responds the same
// way regardless of whether the account exists, to prevent enumeration. When
// the reset is issued, we log the URL so developers can pick it up out-of-band
// until SMTP delivery is wired into this code path.
func (h *Handler) RequestPasswordReset(w http.ResponseWriter, r *http.Request) {
	email := strings.TrimSpace(r.FormValue("email"))
	if email == "" {
		h.render(w, r, "forgot_password.html", map[string]interface{}{
			"Title": "Forgot password",
			"Error": "Please enter your email.",
		})
		return
	}

	token, exists, err := h.Core.Reset.RequestReset(r.Context(), email)
	if err != nil {
		slog.Error("request password reset", "error", err)
	}
	if exists && token != "" {
		link := resetLink(r, token)
		// Best-effort email delivery — if SMTP is down we've already persisted
		// the token, so a retry from the user still works. The response to
		// the browser doesn't branch on this either way (anti-enumeration).
		h.Core.Reset.SendResetEmail(email, link)
	}

	h.render(w, r, "forgot_password.html", map[string]interface{}{
		"Title": "Forgot password",
		"Sent":  true,
	})
}

// ResetPasswordPage renders the new-password form, given a valid-looking token
// query parameter. The actual validation happens on submit.
func (h *Handler) ResetPasswordPage(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	h.render(w, r, "reset_password.html", map[string]interface{}{
		"Title": "Reset password",
		"Token": token,
	})
}

// ResetPassword consumes the submitted token + new password.
func (h *Handler) ResetPassword(w http.ResponseWriter, r *http.Request) {
	token := r.FormValue("token")
	password := r.FormValue("password")
	confirm := r.FormValue("confirm")

	if token == "" {
		h.render(w, r, "reset_password.html", map[string]interface{}{
			"Title": "Reset password",
			"Error": "This reset link is missing its token.",
		})
		return
	}
	if password != confirm {
		h.render(w, r, "reset_password.html", map[string]interface{}{
			"Title": "Reset password",
			"Token": token,
			"Error": "Passwords do not match.",
		})
		return
	}

	if err := h.Core.Reset.CompleteReset(r.Context(), token, password); err != nil {
		var msg string
		switch {
		case errors.Is(err, service.ErrInvalidCredentials):
			msg = "This reset link is invalid or has expired. Please request a new one."
		case errors.Is(err, service.ErrWeakPassword):
			msg = "Password must be 8+ characters and include a letter and a number."
		default:
			slog.Error("password reset failed", "error", err)
			msg = "Something went wrong. Please try again."
		}
		h.render(w, r, "reset_password.html", map[string]interface{}{
			"Title": "Reset password",
			"Token": token,
			"Error": msg,
		})
		return
	}

	h.render(w, r, "reset_password.html", map[string]interface{}{
		"Title": "Reset password",
		"Done":  true,
	})
}

func resetLink(r *http.Request, token string) string {
	scheme := "https"
	if r.TLS == nil && r.Header.Get("X-Forwarded-Proto") != "https" {
		scheme = "http"
	}
	if strings.HasPrefix(r.Host, "localhost") || strings.HasPrefix(r.Host, "127.0.0.1") {
		scheme = "http"
	}
	return scheme + "://" + r.Host + "/auth/reset?token=" + token
}
