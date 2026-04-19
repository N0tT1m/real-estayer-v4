package service

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/realestayer/v4/internal/mailer"
	"github.com/realestayer/v4/internal/models"
	"github.com/realestayer/v4/internal/repository"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// NotificationWorker runs periodically to send trip reminders, price-drop
// alerts, and a weekly digest. It's deliberately simple: polls the DB each
// tick, figures out what's due, and sends email via the shared mailer.
//
// Users opt in/out via UserPreferences.Notifications. Per-item dedup is
// stamped on the User doc under `preferences.notifications.sent` so we don't
// email the same reminder twice.
type NotificationWorker struct {
	users         *repository.UserRepository
	trips         *repository.TripRepository
	watchlist     *repository.WatchlistRepository
	priceHistory  *repository.PriceHistoryRepository
	flightStatus  *FlightStatusService
	mailer        *mailer.Mailer
	appBaseURL    string
}

func NewNotificationWorker(
	users *repository.UserRepository,
	trips *repository.TripRepository,
	watchlist *repository.WatchlistRepository,
	priceHistory *repository.PriceHistoryRepository,
	flightStatus *FlightStatusService,
	m *mailer.Mailer,
	appBaseURL string,
) *NotificationWorker {
	return &NotificationWorker{
		users: users, trips: trips, watchlist: watchlist,
		priceHistory: priceHistory, flightStatus: flightStatus,
		mailer: m, appBaseURL: strings.TrimRight(appBaseURL, "/"),
	}
}

// Run performs one pass of all notification checks. Returns a summary for
// logging; errors per-user are logged but don't abort the pass.
type NotifyRunSummary struct {
	TripReminders  int
	PriceDrops     int
	FlightAlerts   int
	WeeklyDigests  int
}

func (w *NotificationWorker) Run(ctx context.Context) NotifyRunSummary {
	summary := NotifyRunSummary{}
	if w.mailer == nil || !w.mailer.Configured() {
		return summary
	}

	summary.TripReminders = w.sendTripReminders(ctx)
	summary.PriceDrops = w.sendPriceDrops(ctx)
	summary.FlightAlerts = w.sendFlightAlerts(ctx)
	summary.WeeklyDigests = w.sendWeeklyDigests(ctx)
	return summary
}

// ---- Trip reminders ----

// sendTripReminders emails the owner of each trip at T-7 and T-1 days.
// Dedup is via a per-trip entry in the user's notifications.sent map — we
// read + write through UserRepository.Update.
func (w *NotificationWorker) sendTripReminders(ctx context.Context) int {
	sent := 0
	now := time.Now()
	// Loose upper bound on how many upcoming trips we'll iterate per user.
	cursor, err := w.trips.FindUpcomingAll(ctx, now, 500)
	if err != nil {
		slog.Warn("notifications: list upcoming trips", "error", err)
		return 0
	}

	for _, trip := range cursor {
		delta := trip.StartDate.Sub(now).Hours() / 24
		var key string
		switch {
		case delta >= 6 && delta <= 7.5:
			key = "reminder_7d"
		case delta >= 0 && delta <= 1.5:
			key = "reminder_1d"
		default:
			continue
		}
		user, err := w.users.FindByID(ctx, trip.UserID)
		if err != nil || user == nil {
			continue
		}
		if !user.Preferences.Notifications.TripReminders {
			continue
		}
		if alreadySent(user, trip.ID, key) {
			continue
		}
		subject := fmt.Sprintf("Your trip to %s is coming up", tripHeadline(trip))
		if key == "reminder_7d" {
			subject = "One week to go — " + subject
		}
		body := w.tripReminderBody(trip, key)
		if err := w.mailer.Send(mailer.Message{
			To: []string{user.Email}, Subject: subject,
			Text: body.text, HTML: body.html,
		}); err != nil {
			slog.Warn("notifications: reminder send failed", "error", err, "user", user.Email)
			continue
		}
		recordSent(user, trip.ID, key)
		if err := w.users.Update(ctx, user); err != nil {
			slog.Warn("notifications: user update after send failed", "user_id", user.ID.Hex(), "error", err)
		}
		sent++
	}
	return sent
}

// ---- Price drops ----

// sendPriceDrops walks watchlist items whose latest price point is lower
// than the previous one by >3% and emails the owner.
func (w *NotificationWorker) sendPriceDrops(ctx context.Context) int {
	items, err := w.watchlist.GetAllActive(ctx)
	if err != nil {
		slog.Warn("notifications: list watchlist", "error", err)
		return 0
	}
	sent := 0
	for _, item := range items {
		points, err := w.priceHistory.Recent(ctx, item.ListingID, 2)
		if err != nil || len(points) < 2 {
			continue
		}
		prev, latest := points[0], points[1]
		if latest.Price >= prev.Price {
			continue
		}
		delta := (prev.Price - latest.Price) / prev.Price
		if delta < 0.03 {
			continue
		}
		user, err := w.users.FindByID(ctx, item.UserID)
		if err != nil || user == nil {
			continue
		}
		if !user.Preferences.Notifications.PriceAlerts {
			continue
		}
		key := fmt.Sprintf("price_drop_%d", int(latest.CapturedAt.Unix()))
		if alreadySent(user, item.ID, key) {
			continue
		}
		subject := fmt.Sprintf("Price drop on a stay you're tracking (−%.0f%%)", delta*100)
		text := fmt.Sprintf(
			"A listing on your watchlist dropped from %.2f to %.2f %s.\n\nView it: %s/watchlist",
			prev.Price, latest.Price, latest.Currency, w.appBaseURL,
		)
		html := fmt.Sprintf(
			`<p>A listing on your watchlist dropped <strong>%.0f%%</strong>:</p>
			 <p>%.2f → <strong>%.2f %s</strong></p>
			 <p><a href="%s/watchlist">See your watchlist</a></p>`,
			delta*100, prev.Price, latest.Price, latest.Currency, w.appBaseURL)

		if err := w.mailer.Send(mailer.Message{
			To: []string{user.Email}, Subject: subject, Text: text, HTML: html,
		}); err != nil {
			slog.Warn("notifications: price drop send failed", "error", err)
			continue
		}
		recordSent(user, item.ID, key)
		if err := w.users.Update(ctx, user); err != nil {
			slog.Warn("notifications: user update after send failed", "user_id", user.ID.Hex(), "error", err)
		}
		sent++
	}
	return sent
}

// ---- Flight alerts ----

// sendFlightAlerts checks flights within the next 24h for delays via
// AviationStack. Requires the flight item's ReferenceID to be the IATA
// flight number (e.g. "UA283"). Graceful when unconfigured.
func (w *NotificationWorker) sendFlightAlerts(ctx context.Context) int {
	if w.flightStatus == nil || !w.flightStatus.Configured() {
		return 0
	}
	now := time.Now()
	windowEnd := now.Add(24 * time.Hour)
	trips, err := w.trips.FindUpcomingAll(ctx, now, 200)
	if err != nil {
		return 0
	}
	sent := 0
	for _, trip := range trips {
		for _, item := range trip.Items {
			if item.Type != models.TripItemTypeFlight || item.StartTime == nil {
				continue
			}
			if item.StartTime.After(windowEnd) || item.StartTime.Before(now) {
				continue
			}
			if item.ReferenceID == "" {
				continue
			}
			user, err := w.users.FindByID(ctx, trip.UserID)
			if err != nil || user == nil {
				continue
			}
			status, err := w.flightStatus.Lookup(ctx, item.ReferenceID, item.StartTime.Format("2006-01-02"))
			if err != nil || status == nil {
				continue
			}
			if status.DelayMinutes < 15 && status.Status != "cancelled" && status.Status != "diverted" {
				continue
			}
			key := fmt.Sprintf("flight_alert_%s_%d", status.Status, status.DelayMinutes)
			if alreadySent(user, item.ID, key) {
				continue
			}
			subject := fmt.Sprintf("Flight %s: %s", item.ReferenceID, strings.Title(status.Status))
			if status.DelayMinutes > 0 {
				subject = fmt.Sprintf("Flight %s delayed %d min", item.ReferenceID, status.DelayMinutes)
			}
			text := fmt.Sprintf("%s %s → %s, scheduled %s.\nCurrent status: %s.\n",
				status.Airline, status.DepIATA, status.ArrIATA,
				status.DepScheduled.Format("Mon Jan 2 15:04"), status.Status,
			)
			if err := w.mailer.Send(mailer.Message{
				To: []string{user.Email}, Subject: subject, Text: text,
			}); err != nil {
				slog.Warn("notifications: flight alert send failed", "error", err)
				continue
			}
			recordSent(user, item.ID, key)
			if err := w.users.Update(ctx, user); err != nil {
			slog.Warn("notifications: user update after send failed", "user_id", user.ID.Hex(), "error", err)
		}
			sent++
		}
	}
	return sent
}

// ---- Weekly digest ----

// sendWeeklyDigests sends a Monday-morning recap of upcoming trips. We dedup
// per ISO week so each user sees exactly one per week even if the worker
// ticks frequently.
func (w *NotificationWorker) sendWeeklyDigests(ctx context.Context) int {
	now := time.Now()
	if now.Weekday() != time.Monday {
		return 0
	}
	year, week := now.ISOWeek()
	sent := 0

	// Fan out by listing every user — cheap if the user count is small; if
	// this ever becomes a hot path, paginate.
	users, _, err := w.users.List(ctx, 1, 10000)
	if err != nil {
		return 0
	}
	for _, u := range users {
		if !u.Preferences.Notifications.TripReminders {
			continue
		}
		key := fmt.Sprintf("weekly_digest_%d_%02d", year, week)
		if alreadySentForUser(&u, key) {
			continue
		}
		upcoming, err := w.trips.GetUpcoming(ctx, u.ID, 5)
		if err != nil || len(upcoming) == 0 {
			continue
		}
		var body strings.Builder
		fmt.Fprintf(&body, "<p>Hi %s — here's what's coming up:</p><ul>", u.Name)
		var text strings.Builder
		fmt.Fprintf(&text, "Hi %s — here's what's coming up:\n", u.Name)
		for _, t := range upcoming {
			fmt.Fprintf(&body, "<li><strong>%s</strong> — %s</li>", t.Name, t.StartDate.Format("Mon Jan 2"))
			fmt.Fprintf(&text, "- %s on %s\n", t.Name, t.StartDate.Format("Mon Jan 2"))
		}
		body.WriteString("</ul>")

		if err := w.mailer.Send(mailer.Message{
			To: []string{u.Email}, Subject: "Your Real-Estayer week ahead",
			Text: text.String(), HTML: body.String(),
		}); err != nil {
			continue
		}
		recordSentForUser(&u, key)
		if err := w.users.Update(ctx, &u); err != nil {
			slog.Warn("notifications: user update after weekly digest failed", "user_id", u.ID.Hex(), "error", err)
		}
		sent++
	}
	return sent
}

// ---- helpers ----

type emailBody struct {
	text string
	html string
}

func (w *NotificationWorker) tripReminderBody(trip models.Trip, kind string) emailBody {
	banner := "Your trip is one week away."
	if kind == "reminder_1d" {
		banner = "Your trip starts tomorrow!"
	}
	linesText := []string{banner, ""}
	linesText = append(linesText, "Trip: "+trip.Name)
	linesText = append(linesText, "Dates: "+trip.StartDate.Format("Mon Jan 2")+" – "+trip.EndDate.Format("Mon Jan 2"))
	linesText = append(linesText, w.appBaseURL+"/trips/"+trip.ID.Hex())

	html := fmt.Sprintf(`<p>%s</p>
<p><strong>%s</strong><br>%s – %s</p>
<p><a href="%s/trips/%s" style="background:#2563eb;color:#fff;padding:10px 16px;border-radius:8px;text-decoration:none;display:inline-block">Open trip</a></p>`,
		banner, trip.Name, trip.StartDate.Format("Mon Jan 2"), trip.EndDate.Format("Mon Jan 2"),
		w.appBaseURL, trip.ID.Hex())
	return emailBody{text: strings.Join(linesText, "\n"), html: html}
}

func tripHeadline(t models.Trip) string {
	if len(t.Destinations) > 0 {
		return t.Destinations[0].Name
	}
	return t.Name
}

// alreadySent / recordSent use the notifications.sent map on User. Keys are
// "<item-id>:<kind>".
func alreadySent(user *models.User, id primitive.ObjectID, kind string) bool {
	if user == nil || user.Preferences.Notifications.Sent == nil {
		return false
	}
	_, ok := user.Preferences.Notifications.Sent[id.Hex()+":"+kind]
	return ok
}
func recordSent(user *models.User, id primitive.ObjectID, kind string) {
	if user.Preferences.Notifications.Sent == nil {
		user.Preferences.Notifications.Sent = map[string]int64{}
	}
	user.Preferences.Notifications.Sent[id.Hex()+":"+kind] = time.Now().Unix()
}

func alreadySentForUser(user *models.User, kind string) bool {
	if user == nil || user.Preferences.Notifications.Sent == nil {
		return false
	}
	_, ok := user.Preferences.Notifications.Sent["user:"+kind]
	return ok
}
func recordSentForUser(user *models.User, kind string) {
	if user.Preferences.Notifications.Sent == nil {
		user.Preferences.Notifications.Sent = map[string]int64{}
	}
	user.Preferences.Notifications.Sent["user:"+kind] = time.Now().Unix()
}
