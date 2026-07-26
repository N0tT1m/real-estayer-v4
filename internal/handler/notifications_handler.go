package handler

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/realestayer/v4/internal/middleware"
	"github.com/realestayer/v4/internal/models"
	"github.com/realestayer/v4/internal/service"
)

// UpdateNotifications persists the user's notification preferences, including
// the optional per-user Discord webhook. Accepts either JSON or form-encoded
// bodies so the existing profile form keeps working.
func (h *Handler) UpdateNotifications(w http.ResponseWriter, r *http.Request) {
	var prefs models.NotificationSettings

	if r.Header.Get("Content-Type") == "application/json" {
		if err := h.parseJSON(r, &prefs); err != nil {
			h.jsonError(w, http.StatusBadRequest, "Invalid request body")
			return
		}
	} else {
		prefs = models.NotificationSettings{
			PriceAlerts:    r.FormValue("price_alerts") == "on" || r.FormValue("price_alerts") == "true",
			TripReminders:  r.FormValue("trip_reminders") == "on" || r.FormValue("trip_reminders") == "true",
			Marketing:      r.FormValue("marketing") == "on" || r.FormValue("marketing") == "true",
			DiscordEnabled: r.FormValue("discord_enabled") == "on" || r.FormValue("discord_enabled") == "true",
			DiscordWebhook: r.FormValue("discord_webhook"),
		}
	}

	user, err := h.Core.User.UpdateNotifications(r.Context(), h.getUserID(r), prefs)
	if err != nil {
		if errors.Is(err, service.ErrInvalidDiscordWebhook()) {
			h.jsonError(w, http.StatusBadRequest, err.Error())
			return
		}
		h.jsonError(w, http.StatusInternalServerError, "Failed to update notifications")
		return
	}
	h.jsonResponse(w, http.StatusOK, user.Preferences.Notifications)
}

// TestDiscordWebhook posts a test message to the caller's saved webhook (or
// the global fallback) so users can verify their configuration.
func (h *Handler) TestDiscordWebhook(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r.Context())
	webhook := ""
	if user != nil {
		webhook = user.Preferences.Notifications.DiscordWebhook
	}
	if webhook == "" {
		webhook = h.Config.DiscordWebhookURL
	}
	if webhook == "" {
		h.jsonError(w, http.StatusServiceUnavailable, "No Discord webhook configured. Add one in your profile.")
		return
	}

	body, _ := json.Marshal(map[string]string{
		"content": ":white_check_mark: Real-Estayer test — your Discord webhook is working.",
	})
	req, err := http.NewRequestWithContext(r.Context(), http.MethodPost, webhook, bytes.NewReader(body))
	if err != nil {
		h.jsonError(w, http.StatusInternalServerError, "Failed to build request")
		return
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		h.jsonError(w, http.StatusBadGateway, "Could not reach Discord: "+err.Error())
		return
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 400 {
		h.jsonError(w, http.StatusBadGateway, "Discord returned an error. Double-check the webhook URL.")
		return
	}
	h.jsonResponse(w, http.StatusOK, map[string]string{"message": "Test message delivered."})
}
