package handler

import (
	"net/http"
	"time"

	"github.com/realestayer/v3/internal/models"
	"github.com/realestayer/v3/internal/service"
)

// LoginPage renders the login page
func (h *Handler) LoginPage(w http.ResponseWriter, r *http.Request) {
	h.render(w, r, "login.html", map[string]interface{}{
		"Title":    "Login",
		"Redirect": r.URL.Query().Get("redirect"),
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

	// Check content type
	if r.Header.Get("Content-Type") == "application/json" {
		if err := h.parseJSON(r, &req); err != nil {
			h.jsonError(w, http.StatusBadRequest, "Invalid request body")
			return
		}
	} else {
		// Form submission
		req.Email = r.FormValue("email")
		req.Password = r.FormValue("password")
	}

	if req.Email == "" || req.Password == "" {
		if r.Header.Get("Content-Type") == "application/json" {
			h.jsonError(w, http.StatusBadRequest, "Email and password are required")
		} else {
			h.render(w, r, "login.html", map[string]interface{}{
				"Title": "Login",
				"Error": "Email and password are required",
			})
		}
		return
	}

	resp, err := h.authService.Login(r.Context(), req, r.RemoteAddr, r.UserAgent())
	if err != nil {
		if err == service.ErrInvalidCredentials {
			if r.Header.Get("Content-Type") == "application/json" {
				h.jsonError(w, http.StatusUnauthorized, "Invalid email or password")
			} else {
				h.render(w, r, "login.html", map[string]interface{}{
					"Title": "Login",
					"Error": "Invalid email or password",
					"Email": req.Email,
				})
			}
			return
		}
		h.jsonError(w, http.StatusInternalServerError, "Login failed")
		return
	}

	// Set session cookie
	http.SetCookie(w, &http.Cookie{
		Name:     "session_token",
		Value:    resp.Token,
		Path:     "/",
		Expires:  time.Now().Add(7 * 24 * time.Hour),
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})

	if r.Header.Get("Content-Type") == "application/json" {
		h.jsonResponse(w, http.StatusOK, resp)
	} else {
		redirect := r.FormValue("redirect")
		if redirect == "" {
			redirect = "/dashboard"
		}
		http.Redirect(w, r, redirect, http.StatusFound)
	}
}

// Register handles user registration
func (h *Handler) Register(w http.ResponseWriter, r *http.Request) {
	var req models.RegisterRequest

	if r.Header.Get("Content-Type") == "application/json" {
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
		if r.Header.Get("Content-Type") == "application/json" {
			h.jsonError(w, http.StatusBadRequest, "All fields are required")
		} else {
			h.render(w, r, "register.html", map[string]interface{}{
				"Title": "Register",
				"Error": "All fields are required",
			})
		}
		return
	}

	if len(req.Password) < 8 {
		if r.Header.Get("Content-Type") == "application/json" {
			h.jsonError(w, http.StatusBadRequest, "Password must be at least 8 characters")
		} else {
			h.render(w, r, "register.html", map[string]interface{}{
				"Title": "Register",
				"Error": "Password must be at least 8 characters",
				"Name":  req.Name,
				"Email": req.Email,
			})
		}
		return
	}

	resp, err := h.authService.Register(r.Context(), req)
	if err != nil {
		if err == service.ErrEmailTaken {
			if r.Header.Get("Content-Type") == "application/json" {
				h.jsonError(w, http.StatusConflict, "Email already registered")
			} else {
				h.render(w, r, "register.html", map[string]interface{}{
					"Title": "Register",
					"Error": "Email already registered",
					"Name":  req.Name,
				})
			}
			return
		}
		h.jsonError(w, http.StatusInternalServerError, "Registration failed")
		return
	}

	// Set session cookie
	http.SetCookie(w, &http.Cookie{
		Name:     "session_token",
		Value:    resp.Token,
		Path:     "/",
		Expires:  time.Now().Add(7 * 24 * time.Hour),
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})

	if r.Header.Get("Content-Type") == "application/json" {
		h.jsonResponse(w, http.StatusCreated, resp)
	} else {
		http.Redirect(w, r, "/dashboard", http.StatusFound)
	}
}

// Logout handles user logout
func (h *Handler) Logout(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie("session_token")
	if err == nil {
		_ = h.authService.Logout(r.Context(), cookie.Value)
	}

	// Clear cookie
	http.SetCookie(w, &http.Cookie{
		Name:     "session_token",
		Value:    "",
		Path:     "/",
		Expires:  time.Unix(0, 0),
		HttpOnly: true,
	})

	if r.Header.Get("Content-Type") == "application/json" {
		h.jsonResponse(w, http.StatusOK, map[string]string{"message": "Logged out"})
	} else {
		http.Redirect(w, r, "/", http.StatusFound)
	}
}
