package middleware

import (
	"context"
	"net/http"
	"strings"

	"github.com/realestayer/v3/internal/models"
	"github.com/realestayer/v3/internal/service"
)

type contextKey string

const (
	UserContextKey    contextKey = "user"
	SessionContextKey contextKey = "session"
)

// RequireAuth is middleware that requires a valid session token
func RequireAuth(authService *service.AuthService) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token := extractToken(r)
			if token == "" {
				// Check if this is an API request or page request
				if strings.HasPrefix(r.URL.Path, "/api/") {
					http.Error(w, `{"error": "unauthorized"}`, http.StatusUnauthorized)
				} else {
					http.Redirect(w, r, "/auth/login?redirect="+r.URL.Path, http.StatusFound)
				}
				return
			}

			user, session, err := authService.ValidateSession(r.Context(), token)
			if err != nil {
				if strings.HasPrefix(r.URL.Path, "/api/") {
					http.Error(w, `{"error": "invalid or expired session"}`, http.StatusUnauthorized)
				} else {
					http.Redirect(w, r, "/auth/login?redirect="+r.URL.Path, http.StatusFound)
				}
				return
			}

			// Add user and session to context
			ctx := context.WithValue(r.Context(), UserContextKey, user)
			ctx = context.WithValue(ctx, SessionContextKey, session)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// RequireAdmin is middleware that requires the user to be an admin
func RequireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user := GetUser(r.Context())
		if user == nil || !user.IsAdmin {
			if strings.HasPrefix(r.URL.Path, "/api/") {
				http.Error(w, `{"error": "forbidden"}`, http.StatusForbidden)
			} else {
				http.Error(w, "Forbidden", http.StatusForbidden)
			}
			return
		}
		next.ServeHTTP(w, r)
	})
}

// OptionalAuth is middleware that validates session if present but doesn't require it
func OptionalAuth(authService *service.AuthService) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token := extractToken(r)
			if token != "" {
				user, session, err := authService.ValidateSession(r.Context(), token)
				if err == nil {
					ctx := context.WithValue(r.Context(), UserContextKey, user)
					ctx = context.WithValue(ctx, SessionContextKey, session)
					r = r.WithContext(ctx)
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

// extractToken gets the session token from cookie or Authorization header
func extractToken(r *http.Request) string {
	// Check cookie first
	if cookie, err := r.Cookie("session_token"); err == nil {
		return cookie.Value
	}

	// Check Authorization header
	authHeader := r.Header.Get("Authorization")
	if strings.HasPrefix(authHeader, "Bearer ") {
		return strings.TrimPrefix(authHeader, "Bearer ")
	}

	return ""
}

// GetUser retrieves the user from context
func GetUser(ctx context.Context) *models.User {
	user, ok := ctx.Value(UserContextKey).(*models.User)
	if !ok {
		return nil
	}
	return user
}

// GetSession retrieves the session from context
func GetSession(ctx context.Context) *models.Session {
	session, ok := ctx.Value(SessionContextKey).(*models.Session)
	if !ok {
		return nil
	}
	return session
}

// GetUserID returns the current user's ID or zero value
func GetUserID(ctx context.Context) string {
	user := GetUser(ctx)
	if user == nil {
		return ""
	}
	return user.ID.Hex()
}
