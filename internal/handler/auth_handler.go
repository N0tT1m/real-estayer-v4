package handler

import (
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/realestayer/v4/internal/models"
	"github.com/realestayer/v4/internal/service"
)

const sessionCookieName = "session_token"
const sessionCookieMaxAge = 7 * 24 * time.Hour

// LoginPage renders the login page
func (h *Handler) LoginPage(w http.ResponseWriter, r *http.Request) {
	h.render(w, r, "login.html", map[string]interface{}{
		"Title":    "Login",
		"Redirect": sanitizeRedirect(r.URL.Query().Get("redirect")),
	})
}

// RegisterPage renders the registration page
func (h *Handler) RegisterPage(w http.ResponseWriter, r *http.Request) {
	h.render(w, r, "register.html", map[string]interface{}{
		"Title": "Register",
	})
}

// Login handles user login
func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	var req models.LoginRequest

	isJSON := strings.HasPrefix(r.Header.Get("Content-Type"), "application/json")
	if isJSON {
		if err := h.parseJSON(r, &req); err != nil {
			h.jsonError(w, http.StatusBadRequest, "Invalid request body")
			return
		}
	} else {
		req.Email = r.FormValue("email")
		req.Password = r.FormValue("password")
	}

	if req.Email == "" || req.Password == "" {
		if isJSON {
			h.jsonError(w, http.StatusBadRequest, "Email and password are required")
		} else {
			h.render(w, r, "login.html", map[string]interface{}{
				"Title": "Login",
				"Error": "Email and password are required",
			})
		}
		return
	}

	totpCode := r.FormValue("totp")
	if isJSON {
		// JSON clients can put the code on the request struct or top-level.
		totpCode = strings.TrimSpace(totpCode)
		if totpCode == "" {
			var extra struct {
				TOTPCode string `json:"totp"`
			}
			_ = h.parseJSON(r, &extra) // best-effort
			totpCode = extra.TOTPCode
		}
	}

	resp, err := h.Core.Auth.Login(r.Context(), req, r.RemoteAddr, r.UserAgent(), totpCode)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrTOTPRequired):
			if isJSON {
				h.jsonResponse(w, http.StatusUnauthorized, map[string]string{"error": "totp_required"})
			} else {
				h.render(w, r, "login.html", map[string]interface{}{
					"Title":      "Two-factor code required",
					"Email":      req.Email,
					"TOTPPrompt": true,
				})
			}
			return
		case errors.Is(err, service.ErrTOTPInvalid):
			if isJSON {
				h.jsonError(w, http.StatusUnauthorized, "Invalid two-factor code")
			} else {
				h.render(w, r, "login.html", map[string]interface{}{
					"Title":      "Two-factor code required",
					"Error":      "That code didn't match. Try again.",
					"Email":      req.Email,
					"TOTPPrompt": true,
				})
			}
			return
		case errors.Is(err, service.ErrInvalidCredentials):
			if isJSON {
				h.jsonError(w, http.StatusUnauthorized, "Invalid credentials")
			} else {
				h.render(w, r, "login.html", map[string]interface{}{
					"Title": "Login",
					"Error": "Invalid credentials",
					"Email": req.Email,
				})
			}
			return
		}
		slog.Error("login failed", "error", err)
		h.jsonError(w, http.StatusInternalServerError, "Login failed")
		return
	}

	h.setSessionCookie(w, resp.Token)

	if isJSON {
		h.jsonResponse(w, http.StatusOK, resp)
	} else {
		// #nosec G710 -- sanitizeRedirect rejects absolute URLs, scheme/host,
		// and protocol-relative ("//", "/\\") forms; only same-site paths pass.
		redirect := sanitizeRedirect(r.FormValue("redirect"))
		if redirect == "" {
			redirect = "/dashboard"
		}
		http.Redirect(w, r, redirect, http.StatusFound) // #nosec G710 -- sanitizeRedirect allows same-site paths only
	}
}

// Register handles user registration
func (h *Handler) Register(w http.ResponseWriter, r *http.Request) {
	var req models.RegisterRequest

	isJSON := strings.HasPrefix(r.Header.Get("Content-Type"), "application/json")
	if isJSON {
		if err := h.parseJSON(r, &req); err != nil {
			h.jsonError(w, http.StatusBadRequest, "Invalid request body")
			return
		}
	} else {
		req.Email = r.FormValue("email")
		req.Password = r.FormValue("password")
		req.Name = r.FormValue("name")
	}

	if req.Email == "" || req.Password == "" || req.Name == "" {
		if isJSON {
			h.jsonError(w, http.StatusBadRequest, "All fields are required")
		} else {
			h.render(w, r, "register.html", map[string]interface{}{
				"Title": "Register",
				"Error": "All fields are required",
			})
		}
		return
	}

	resp, err := h.Core.Auth.Register(r.Context(), req)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrEmailTaken):
			// Anti-enumeration: generic message that doesn't confirm account existence.
			msg := "Unable to register with those details. Please try again."
			if isJSON {
				h.jsonError(w, http.StatusBadRequest, msg)
			} else {
				h.render(w, r, "register.html", map[string]interface{}{
					"Title": "Register",
					"Error": msg,
					"Name":  req.Name,
				})
			}
		case errors.Is(err, service.ErrInvalidEmail):
			h.renderRegisterError(w, r, isJSON, req, "Please enter a valid email address.")
		case errors.Is(err, service.ErrWeakPassword):
			h.renderRegisterError(w, r, isJSON, req, "Password must be 8+ characters and include a letter and a number.")
		default:
			slog.Error("register failed", "error", err)
			h.jsonError(w, http.StatusInternalServerError, "Registration failed")
		}
		return
	}

	h.setSessionCookie(w, resp.Token)

	if isJSON {
		h.jsonResponse(w, http.StatusCreated, resp)
	} else {
		http.Redirect(w, r, "/dashboard", http.StatusFound)
	}
}

func (h *Handler) renderRegisterError(w http.ResponseWriter, r *http.Request, isJSON bool, req models.RegisterRequest, msg string) {
	if isJSON {
		h.jsonError(w, http.StatusBadRequest, msg)
		return
	}
	h.render(w, r, "register.html", map[string]interface{}{
		"Title": "Register",
		"Error": msg,
		"Name":  req.Name,
		"Email": req.Email,
	})
}

// Logout handles user logout
func (h *Handler) Logout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(sessionCookieName); err == nil {
		if err := h.Core.Auth.Logout(r.Context(), cookie.Value); err != nil {
			slog.Warn("logout failed to invalidate session", "error", err)
		}
	}

	// #nosec G124 -- HttpOnly/SameSite are set below; Secure is env-dependent
	// (IsProduction) which gosec cannot evaluate.
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		Expires:  time.Unix(0, 0),
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   h.Config.IsProduction(),
		SameSite: http.SameSiteStrictMode,
	})

	if strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		h.jsonResponse(w, http.StatusOK, map[string]string{"message": "Logged out"})
	} else {
		http.Redirect(w, r, "/", http.StatusFound)
	}
}

func (h *Handler) setSessionCookie(w http.ResponseWriter, token string) {
	// #nosec G124 -- HttpOnly/SameSite are set below; Secure is env-dependent
	// (IsProduction) which gosec cannot evaluate.
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    token,
		Path:     "/",
		Expires:  time.Now().Add(sessionCookieMaxAge),
		MaxAge:   int(sessionCookieMaxAge.Seconds()),
		HttpOnly: true,
		Secure:   h.Config.IsProduction(),
		SameSite: http.SameSiteStrictMode,
	})
}

// sanitizeRedirect returns the input only if it's a safe internal path. Anything
// absolute, scheme-bearing, or protocol-relative is dropped to prevent open
// redirects.
func sanitizeRedirect(s string) string {
	if s == "" {
		return ""
	}
	if strings.HasPrefix(s, "//") || strings.HasPrefix(s, "/\\") {
		return ""
	}
	if !strings.HasPrefix(s, "/") {
		return ""
	}
	parsed, err := url.Parse(s)
	if err != nil {
		return ""
	}
	if parsed.Scheme != "" || parsed.Host != "" {
		return ""
	}
	return parsed.RequestURI()
}
