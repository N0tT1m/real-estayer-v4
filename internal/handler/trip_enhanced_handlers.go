package handler

import (
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/realestayer/v4/internal/middleware"
	"github.com/realestayer/v4/internal/models"
	"github.com/realestayer/v4/internal/service"
)

// ===== ICS export =====

func (h *Handler) TripICS(w http.ResponseWriter, r *http.Request) {
	tripID := chi.URLParam(r, "id")
	userID := h.getUserID(r)
	trip, err := h.tripService.GetByID(r.Context(), userID, tripID)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "text/calendar; charset=utf-8")
	// Build the filename from the trip's own ID rather than the raw URL
	// parameter. Today GetByID rejects anything that isn't a valid ObjectID,
	// so tripID is already safe hex — but relying on an upstream check to keep
	// quotes out of a quoted header value is fragile.
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s.ics"`, trip.ID.Hex()))
	_, _ = w.Write([]byte(service.RenderTripICS(trip))) // #nosec G705 -- text/calendar attachment, not rendered as HTML
}

// ===== Reorder =====

type reorderItemsReq struct {
	ItemIDs []string `json:"item_ids"`
}

func (h *Handler) TripReorderItems(w http.ResponseWriter, r *http.Request) {
	var req reorderItemsReq
	if err := h.parseJSON(r, &req); err != nil {
		h.jsonError(w, http.StatusBadRequest, "invalid body")
		return
	}
	if err := h.tripService.ReorderItems(r.Context(), h.getUserID(r), chi.URLParam(r, "id"), req.ItemIDs); err != nil {
		h.jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	h.jsonResponse(w, http.StatusOK, map[string]string{"message": "reordered"})
}

// ===== Collaborators =====

type addCollabReq struct {
	Email string          `json:"email"`
	Role  models.TripRole `json:"role"`
}

func (h *Handler) TripAddCollaborator(w http.ResponseWriter, r *http.Request) {
	var req addCollabReq
	if err := h.parseJSON(r, &req); err != nil {
		h.jsonError(w, http.StatusBadRequest, "invalid body")
		return
	}
	c, err := h.tripService.AddCollaboratorByEmail(r.Context(), h.getUserID(r), chi.URLParam(r, "id"), req.Email, req.Role, h.users)
	if err != nil {
		h.jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	h.jsonResponse(w, http.StatusCreated, c)
}

func (h *Handler) TripRemoveCollaborator(w http.ResponseWriter, r *http.Request) {
	if err := h.tripService.RemoveCollaborator(r.Context(), h.getUserID(r), chi.URLParam(r, "id"), chi.URLParam(r, "userId")); err != nil {
		h.jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	h.jsonResponse(w, http.StatusOK, map[string]string{"message": "removed"})
}

// ===== Clone =====

type cloneTripReq struct {
	StartDate time.Time `json:"start_date"`
	Name      string    `json:"name"`
}

func (h *Handler) TripClone(w http.ResponseWriter, r *http.Request) {
	var req cloneTripReq
	if err := h.parseJSON(r, &req); err != nil {
		h.jsonError(w, http.StatusBadRequest, "invalid body")
		return
	}
	trip, err := h.tripService.Clone(r.Context(), h.getUserID(r), chi.URLParam(r, "id"), req.StartDate, req.Name)
	if err != nil {
		h.jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	h.jsonResponse(w, http.StatusCreated, trip)
}

// ===== Packing & Checklist =====

func (h *Handler) TripSetPackingList(w http.ResponseWriter, r *http.Request) {
	var items []models.PackingItem
	if err := h.parseJSON(r, &items); err != nil {
		h.jsonError(w, http.StatusBadRequest, "invalid body")
		return
	}
	if err := h.tripService.SetPackingList(r.Context(), h.getUserID(r), chi.URLParam(r, "id"), items); err != nil {
		h.jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	h.jsonResponse(w, http.StatusOK, map[string]string{"message": "saved"})
}

func (h *Handler) TripSetChecklist(w http.ResponseWriter, r *http.Request) {
	var items []models.ChecklistItem
	if err := h.parseJSON(r, &items); err != nil {
		h.jsonError(w, http.StatusBadRequest, "invalid body")
		return
	}
	if err := h.tripService.SetChecklist(r.Context(), h.getUserID(r), chi.URLParam(r, "id"), items); err != nil {
		h.jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	h.jsonResponse(w, http.StatusOK, map[string]string{"message": "saved"})
}

func (h *Handler) TripSuggestPacking(w http.ResponseWriter, r *http.Request) {
	trip, err := h.tripService.GetByID(r.Context(), h.getUserID(r), chi.URLParam(r, "id"))
	if err != nil {
		h.jsonError(w, http.StatusNotFound, "not found")
		return
	}
	h.jsonResponse(w, http.StatusOK, service.SuggestPackingList(trip))
}

func (h *Handler) TripSuggestChecklist(w http.ResponseWriter, r *http.Request) {
	trip, err := h.tripService.GetByID(r.Context(), h.getUserID(r), chi.URLParam(r, "id"))
	if err != nil {
		h.jsonError(w, http.StatusNotFound, "not found")
		return
	}
	h.jsonResponse(w, http.StatusOK, service.SuggestChecklist(trip))
}

// ===== Comments =====

type addCommentReq struct {
	ItemID string `json:"item_id,omitempty"`
	Body   string `json:"body"`
}

func (h *Handler) TripListComments(w http.ResponseWriter, r *http.Request) {
	out, err := h.commentService.List(r.Context(), h.getUserID(r), chi.URLParam(r, "id"))
	if err != nil {
		h.jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	h.jsonResponse(w, http.StatusOK, out)
}

func (h *Handler) TripAddComment(w http.ResponseWriter, r *http.Request) {
	var req addCommentReq
	if err := h.parseJSON(r, &req); err != nil {
		h.jsonError(w, http.StatusBadRequest, "invalid body")
		return
	}
	c, err := h.commentService.Add(r.Context(), h.getUserID(r), chi.URLParam(r, "id"), req.ItemID, req.Body)
	if err != nil {
		h.jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	h.jsonResponse(w, http.StatusCreated, c)
}

func (h *Handler) TripDeleteComment(w http.ResponseWriter, r *http.Request) {
	if err := h.commentService.Delete(r.Context(), h.getUserID(r), chi.URLParam(r, "id"), chi.URLParam(r, "commentId")); err != nil {
		h.jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	h.jsonResponse(w, http.StatusOK, map[string]string{"message": "deleted"})
}

// ===== Expenses =====

type createExpenseReq struct {
	Description string              `json:"description"`
	Category    models.TripItemType `json:"category"`
	Amount      float64             `json:"amount"`
	Currency    string              `json:"currency"`
	SpentAt     *time.Time          `json:"spent_at"`
	SplitWith   []string            `json:"split_with"`
}

func (h *Handler) TripListExpenses(w http.ResponseWriter, r *http.Request) {
	out, err := h.expenseService.List(r.Context(), h.getUserID(r), chi.URLParam(r, "id"))
	if err != nil {
		h.jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	h.jsonResponse(w, http.StatusOK, out)
}

func (h *Handler) TripAddExpense(w http.ResponseWriter, r *http.Request) {
	var req createExpenseReq
	if err := h.parseJSON(r, &req); err != nil {
		h.jsonError(w, http.StatusBadRequest, "invalid body")
		return
	}
	e, err := h.expenseService.Create(r.Context(), h.getUserID(r), chi.URLParam(r, "id"), service.CreateExpenseInput{
		Description: req.Description,
		Category:    req.Category,
		Amount:      req.Amount,
		Currency:    req.Currency,
		SpentAt:     req.SpentAt,
		SplitWith:   req.SplitWith,
	})
	if err != nil {
		h.jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	h.jsonResponse(w, http.StatusCreated, e)
}

func (h *Handler) TripDeleteExpense(w http.ResponseWriter, r *http.Request) {
	if err := h.expenseService.Delete(r.Context(), h.getUserID(r), chi.URLParam(r, "id"), chi.URLParam(r, "expenseId")); err != nil {
		h.jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	h.jsonResponse(w, http.StatusOK, map[string]string{"message": "deleted"})
}

func (h *Handler) TripBudgetSummary(w http.ResponseWriter, r *http.Request) {
	summary, expenses, err := h.expenseService.Summary(r.Context(), h.getUserID(r), chi.URLParam(r, "id"))
	if err != nil {
		h.jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	h.jsonResponse(w, http.StatusOK, map[string]interface{}{
		"summary":  summary,
		"expenses": expenses,
	})
}

func (h *Handler) TripSettleUp(w http.ResponseWriter, r *http.Request) {
	balances, currency, err := h.expenseService.SettleUp(r.Context(), h.getUserID(r), chi.URLParam(r, "id"))
	if err != nil {
		h.jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	h.jsonResponse(w, http.StatusOK, map[string]interface{}{
		"balances": balances,
		"currency": currency,
	})
}

// ===== Journal =====

type addJournalReq struct {
	Title     string     `json:"title"`
	Body      string     `json:"body"`
	MediaURLs []string   `json:"media_urls"`
	EntryDate *time.Time `json:"entry_date"`
}

func (h *Handler) TripListJournal(w http.ResponseWriter, r *http.Request) {
	out, err := h.journalService.List(r.Context(), h.getUserID(r), chi.URLParam(r, "id"))
	if err != nil {
		h.jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	h.jsonResponse(w, http.StatusOK, out)
}

func (h *Handler) TripAddJournal(w http.ResponseWriter, r *http.Request) {
	var req addJournalReq
	if err := h.parseJSON(r, &req); err != nil {
		h.jsonError(w, http.StatusBadRequest, "invalid body")
		return
	}
	j, err := h.journalService.Add(r.Context(), h.getUserID(r), chi.URLParam(r, "id"), service.JournalInput{
		Title:     req.Title,
		Body:      req.Body,
		MediaURLs: req.MediaURLs,
		EntryDate: req.EntryDate,
	})
	if err != nil {
		h.jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	h.jsonResponse(w, http.StatusCreated, j)
}

func (h *Handler) TripDeleteJournal(w http.ResponseWriter, r *http.Request) {
	if err := h.journalService.Delete(r.Context(), h.getUserID(r), chi.URLParam(r, "id"), chi.URLParam(r, "entryId")); err != nil {
		h.jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	h.jsonResponse(w, http.StatusOK, map[string]string{"message": "deleted"})
}

// ===== Reviews =====

type upsertReviewReq struct {
	ItemID string `json:"item_id"`
	Rating int    `json:"rating"`
	Body   string `json:"body"`
}

func (h *Handler) TripUpsertReview(w http.ResponseWriter, r *http.Request) {
	var req upsertReviewReq
	if err := h.parseJSON(r, &req); err != nil {
		h.jsonError(w, http.StatusBadRequest, "invalid body")
		return
	}
	rev, err := h.reviewService.Upsert(r.Context(), h.getUserID(r), chi.URLParam(r, "id"), req.ItemID, req.Rating, req.Body)
	if err != nil {
		h.jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	h.jsonResponse(w, http.StatusOK, rev)
}

func (h *Handler) TripListReviews(w http.ResponseWriter, r *http.Request) {
	out, err := h.reviewService.List(r.Context(), h.getUserID(r), chi.URLParam(r, "id"))
	if err != nil {
		h.jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	h.jsonResponse(w, http.StatusOK, out)
}

// ===== Weather =====

func (h *Handler) TripWeather(w http.ResponseWriter, r *http.Request) {
	trip, err := h.tripService.GetByID(r.Context(), h.getUserID(r), chi.URLParam(r, "id"))
	if err != nil {
		h.jsonError(w, http.StatusNotFound, "not found")
		return
	}
	out := map[string]*service.Forecast{}
	for _, d := range trip.Destinations {
		if d.Coordinates == nil {
			continue
		}
		days := daysBetweenTrip(trip)
		if days > 14 {
			days = 14
		}
		f, err := h.weatherService.Get(r.Context(), d.Coordinates.Lat, d.Coordinates.Lng, days)
		if err == nil && f != nil {
			out[d.Name] = f
		}
	}
	h.jsonResponse(w, http.StatusOK, out)
}

func daysBetweenTrip(t *models.Trip) int {
	if t.EndDate.Before(t.StartDate) {
		return 1
	}
	d := int(t.EndDate.Sub(t.StartDate).Hours()/24) + 1
	if d < 1 {
		d = 1
	}
	return d
}

// ===== Currency convert =====

type convertReq struct {
	Amount float64 `json:"amount"`
	From   string  `json:"from"`
	To     string  `json:"to"`
}

func (h *Handler) ConvertCurrency(w http.ResponseWriter, r *http.Request) {
	var req convertReq
	if err := h.parseJSON(r, &req); err != nil {
		h.jsonError(w, http.StatusBadRequest, "invalid body")
		return
	}
	out, err := h.currencyService.Convert(r.Context(), req.Amount, req.From, req.To)
	if err != nil {
		h.jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	h.jsonResponse(w, http.StatusOK, map[string]interface{}{
		"amount":    req.Amount,
		"from":      req.From,
		"to":        req.To,
		"converted": out,
	})
}

// ===== AI itinerary =====

type aiItineraryReq struct {
	Destination string    `json:"destination"`
	StartDate   time.Time `json:"start_date"`
	EndDate     time.Time `json:"end_date"`
	Interests   []string  `json:"interests"`
	BudgetUSD   float64   `json:"budget_usd"`
	Pace        string    `json:"pace"`
}

func (h *Handler) AIItinerary(w http.ResponseWriter, r *http.Request) {
	var req aiItineraryReq
	if err := h.parseJSON(r, &req); err != nil {
		h.jsonError(w, http.StatusBadRequest, "invalid body")
		return
	}
	out, err := h.aiItineraryService.Generate(r.Context(), service.ItineraryRequest{
		Destination: req.Destination,
		StartDate:   req.StartDate,
		EndDate:     req.EndDate,
		Interests:   req.Interests,
		BudgetUSD:   req.BudgetUSD,
		Pace:        req.Pace,
	})
	if err != nil {
		if errors.Is(err, service.ErrAIItineraryNotConfigured) {
			h.jsonError(w, http.StatusServiceUnavailable, "AI itinerary is not configured on this server")
			return
		}
		h.jsonError(w, http.StatusBadGateway, err.Error())
		return
	}
	h.jsonResponse(w, http.StatusOK, out)
}

// ===== Places (Overpass) =====

func (h *Handler) PlacesNearby(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	category := q.Get("category")
	lat, _ := parseFloatParam(q.Get("lat"))
	lng, _ := parseFloatParam(q.Get("lng"))
	radius, _ := parseIntParam(q.Get("radius"))
	out, err := h.placesService.Nearby(r.Context(), category, lat, lng, radius)
	if err != nil {
		h.jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	h.jsonResponse(w, http.StatusOK, map[string]interface{}{"places": out})
}

// ===== Events (Ticketmaster) =====

func (h *Handler) EventsNearby(w http.ResponseWriter, r *http.Request) {
	if !h.eventsService.Configured() {
		h.jsonError(w, http.StatusServiceUnavailable, "events not configured")
		return
	}
	q := r.URL.Query()
	lat, _ := parseFloatParam(q.Get("lat"))
	lng, _ := parseFloatParam(q.Get("lng"))
	radius, _ := parseIntParam(q.Get("radius"))
	limit, _ := parseIntParam(q.Get("limit"))
	var start, end time.Time
	if v := q.Get("start"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			start = t
		}
	}
	if v := q.Get("end"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			end = t
		}
	}
	out, err := h.eventsService.Nearby(r.Context(), lat, lng, radius, start, end, limit)
	if err != nil {
		h.jsonError(w, http.StatusBadGateway, err.Error())
		return
	}
	h.jsonResponse(w, http.StatusOK, map[string]interface{}{"events": out})
}

// ===== Collections =====

func (h *Handler) CollectionsForDestination(w http.ResponseWriter, r *http.Request) {
	dest := r.URL.Query().Get("destination")
	out, err := h.collections.ListForDestination(r.Context(), dest)
	if err != nil {
		h.jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	h.jsonResponse(w, http.StatusOK, map[string]interface{}{"collections": out})
}

func (h *Handler) CollectionsFeatured(w http.ResponseWriter, r *http.Request) {
	limit := 8
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := parseIntParam(v); err == nil {
			limit = n
		}
	}
	out, err := h.collections.ListFeatured(r.Context(), limit)
	if err != nil {
		h.jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	h.jsonResponse(w, http.StatusOK, map[string]interface{}{"collections": out})
}

func (h *Handler) CollectionPage(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	c, err := h.collections.FindBySlug(r.Context(), slug)
	if err != nil {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}
	h.render(w, r, "collection.html", map[string]interface{}{
		"Title":      c.Title,
		"Collection": c,
	})
}

// ===== Misc pages =====

func (h *Handler) AroundMePage(w http.ResponseWriter, r *http.Request) {
	h.render(w, r, "around_me.html", map[string]interface{}{"Title": "Around me"})
}

func (h *Handler) PrintableTripPage(w http.ResponseWriter, r *http.Request) {
	tripID := chi.URLParam(r, "id")
	trip, err := h.tripService.GetByID(r.Context(), h.getUserID(r), tripID)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	h.render(w, r, "trip_print.html", map[string]interface{}{
		"Title": trip.Name + " — printable",
		"Trip":  trip,
	})
}

func (h *Handler) TripCalendarPage(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r.Context())
	trips, _, err := h.tripService.GetUserTrips(r.Context(), userID, 1, 200)
	if err != nil {
		h.jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.render(w, r, "trips_calendar.html", map[string]interface{}{
		"Title": "Trip calendar",
		"Trips": trips,
	})
}

// --- small helpers ---

func parseFloatParam(s string) (float64, error) {
	if s == "" {
		return 0, nil
	}
	var f float64
	_, err := fmt.Sscanf(s, "%f", &f)
	return f, err
}

func parseIntParam(s string) (int, error) {
	if s == "" {
		return 0, nil
	}
	var n int
	_, err := fmt.Sscanf(s, "%d", &n)
	return n, err
}
