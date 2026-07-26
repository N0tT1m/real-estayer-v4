package handler

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/realestayer/v4/internal/models"
	"github.com/realestayer/v4/internal/service"
)

// ---- Email parser ----

type emailImportReq struct {
	Email string `json:"email"`
}

func (h *Handler) ImportEmail(w http.ResponseWriter, r *http.Request) {
	var req emailImportReq
	if err := h.parseJSON(r, &req); err != nil {
		h.jsonError(w, http.StatusBadRequest, "invalid body")
		return
	}
	out, err := h.Trips.EmailParser.Parse(r.Context(), req.Email)
	if err != nil {
		h.jsonError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	h.jsonResponse(w, http.StatusOK, out)
}

// ---- Itinerary conflict check ----

func (h *Handler) TripConflicts(w http.ResponseWriter, r *http.Request) {
	trip, err := h.Trips.Trip.GetByID(r.Context(), h.getUserID(r), chi.URLParam(r, "id"))
	if err != nil {
		h.jsonError(w, http.StatusNotFound, "not found")
		return
	}
	warnings := h.Trips.Conflict.Check(trip)
	h.jsonResponse(w, http.StatusOK, map[string]interface{}{"warnings": warnings})
}

// ---- Visa ----

func (h *Handler) VisaCheck(w http.ResponseWriter, r *http.Request) {
	c := r.URL.Query().Get("citizenship")
	d := chi.URLParam(r, "destination")
	h.jsonResponse(w, http.StatusOK, h.Enrich.Visa.Check(r.Context(), c, d))
}

// ---- Traveler identity ----

func (h *Handler) UpdateTravelerIdentity(w http.ResponseWriter, r *http.Request) {
	var id models.TravelerIdentity
	if err := h.parseJSON(r, &id); err != nil {
		h.jsonError(w, http.StatusBadRequest, "invalid body")
		return
	}
	if err := validateTravelerIdentity(&id); err != nil {
		h.jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	user, err := h.Core.User.UpdateIdentity(r.Context(), h.getUserID(r), id)
	if err != nil {
		h.jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// Intentionally record only which fields were touched, not their values —
	// the audit log must stay free of PII.
	h.Core.Audit.Record(r.Context(), h.getUserOID(r), r, models.AuditActionIdentityUpdated, map[string]any{
		"fields_set": identityFieldsTouched(id),
	})
	h.jsonResponse(w, http.StatusOK, user.Identity)
}

func identityFieldsTouched(id models.TravelerIdentity) []string {
	var out []string
	if id.Citizenship != "" {
		out = append(out, "citizenship")
	}
	if id.PassportNumber != "" {
		out = append(out, "passport_number")
	}
	if id.PassportExpiry != nil {
		out = append(out, "passport_expiry")
	}
	if id.DateOfBirth != nil {
		out = append(out, "date_of_birth")
	}
	if id.KnownTravelerNo != "" {
		out = append(out, "ktn")
	}
	if id.RedressNo != "" {
		out = append(out, "redress_no")
	}
	if id.GlobalEntryID != "" {
		out = append(out, "global_entry_id")
	}
	if len(id.LoyaltyAccounts) > 0 {
		out = append(out, "loyalty_accounts")
	}
	if id.EmergencyContact.Name != "" || id.EmergencyContact.Phone != "" || id.EmergencyContact.Email != "" {
		out = append(out, "emergency_contact")
	}
	return out
}

// validateTravelerIdentity caps every string to a defensive upper bound so we
// don't hand giant payloads to the field cipher or store them in Mongo. The
// limits are loose (well above realistic document numbers) but tight enough
// to refuse a 1 MB abuse attempt.
func validateTravelerIdentity(id *models.TravelerIdentity) error {
	limits := []struct {
		name  string
		value string
		max   int
	}{
		{"citizenship", id.Citizenship, 8},
		{"passport_number", id.PassportNumber, 32},
		{"ktn", id.KnownTravelerNo, 32},
		{"redress_no", id.RedressNo, 32},
		{"global_entry_id", id.GlobalEntryID, 32},
	}
	for _, f := range limits {
		if len(f.value) > f.max {
			return fmt.Errorf("%s too long (max %d)", f.name, f.max)
		}
	}
	if n := len(id.LoyaltyAccounts); n > 50 {
		return fmt.Errorf("too many loyalty accounts (max 50)")
	}
	for i, l := range id.LoyaltyAccounts {
		if len(l.Program) > 80 || len(l.Number) > 40 || len(l.Tier) > 40 {
			return fmt.Errorf("loyalty account %d has field exceeding length limit", i)
		}
	}
	ec := id.EmergencyContact
	if len(ec.Name) > 120 || len(ec.Relationship) > 60 || len(ec.Phone) > 32 || len(ec.Email) > 254 {
		return fmt.Errorf("emergency contact field exceeds length limit")
	}
	return nil
}

// ---- Photo upload ----

// UploadPhoto accepts a multipart file under the "file" field and stores it
// via the configured PhotoStorage, returning a public URL. Max 10 MB. Any
// file larger is rejected at read-time by the storage impl.
func (h *Handler) UploadPhoto(w http.ResponseWriter, r *http.Request) {
	// Enforce the size cap before ParseMultipartForm so a 500 MB body can't
	// exhaust memory on its way to being rejected.
	// #nosec G120 -- body is bounded by MaxBytesReader on the next line.
	r.Body = http.MaxBytesReader(w, r.Body, 12<<20)
	if err := r.ParseMultipartForm(12 << 20); err != nil { // #nosec G120 -- body capped by MaxBytesReader above
		h.jsonError(w, http.StatusBadRequest, "invalid multipart form")
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		h.jsonError(w, http.StatusBadRequest, "missing file field")
		return
	}
	defer func() { _ = file.Close() }()

	// Sniff the magic bytes rather than trusting the client-supplied
	// Content-Type header — the browser-declared type is spoofable.
	sniff := make([]byte, 512)
	n, _ := io.ReadFull(file, sniff)
	detected := http.DetectContentType(sniff[:n])
	if !strings.HasPrefix(detected, "image/") {
		h.jsonError(w, http.StatusUnsupportedMediaType, "only image/* is allowed")
		return
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		h.jsonError(w, http.StatusInternalServerError, "could not rewind upload")
		return
	}
	url, err := h.Core.Photos.Save(r.Context(), h.getUserID(r), header.Filename, file, detected)
	if err != nil {
		h.jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.jsonResponse(w, http.StatusCreated, map[string]string{"url": url, "kind": h.Core.Photos.Kind()})
}

// ---- Transit routing ----

func (h *Handler) Transit(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	fLat, _ := parseFloatParam(q.Get("from_lat"))
	fLng, _ := parseFloatParam(q.Get("from_lng"))
	tLat, _ := parseFloatParam(q.Get("to_lat"))
	tLng, _ := parseFloatParam(q.Get("to_lng"))
	var dep time.Time
	if v := q.Get("depart"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			dep = t
		}
	}
	out, err := h.Enrich.Transit.Route(r.Context(), fLat, fLng, tLat, tLng, dep)
	if err != nil {
		h.jsonError(w, http.StatusBadGateway, err.Error())
		return
	}
	h.jsonResponse(w, http.StatusOK, out)
}

// ---- Polls ----

type createPollReq struct {
	Title       string              `json:"title"`
	Description string              `json:"description"`
	TripID      string              `json:"trip_id"`
	Options     []models.PollOption `json:"options"`
	ClosesAt    *time.Time          `json:"closes_at"`
}

func (h *Handler) CreatePoll(w http.ResponseWriter, r *http.Request) {
	var req createPollReq
	if err := h.parseJSON(r, &req); err != nil {
		h.jsonError(w, http.StatusBadRequest, "invalid body")
		return
	}
	poll, err := h.Trips.Poll.Create(r.Context(), h.getUserID(r), service.CreatePollInput{
		Title: req.Title, Description: req.Description, TripID: req.TripID,
		Options: req.Options, ClosesAt: req.ClosesAt,
	})
	if err != nil {
		h.jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	h.jsonResponse(w, http.StatusCreated, poll)
}

func (h *Handler) ListUserPolls(w http.ResponseWriter, r *http.Request) {
	out, err := h.Trips.Poll.ListForUser(r.Context(), h.getUserID(r))
	if err != nil {
		h.jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	h.jsonResponse(w, http.StatusOK, out)
}

func (h *Handler) DeletePoll(w http.ResponseWriter, r *http.Request) {
	if err := h.Trips.Poll.Delete(r.Context(), h.getUserID(r), chi.URLParam(r, "id")); err != nil {
		h.jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	h.jsonResponse(w, http.StatusOK, map[string]string{"message": "deleted"})
}

type voteReq struct {
	DisplayName string            `json:"display_name"`
	Email       string            `json:"email"`
	OptionVotes map[string]string `json:"option_votes"`
	Note        string            `json:"note"`
}

// PublicPollPage shows a poll to anyone who has the slug (no auth).
func (h *Handler) PublicPollPage(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	poll, err := h.Trips.Poll.GetBySlug(r.Context(), slug)
	if err != nil {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}
	h.render(w, r, "poll.html", map[string]interface{}{
		"Title":   poll.Title,
		"Poll":    poll,
		"Summary": h.Trips.Poll.Summarize(poll),
	})
}

// PublicPollVote accepts a vote from anyone with the slug.
func (h *Handler) PublicPollVote(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	var req voteReq
	if err := h.parseJSON(r, &req); err != nil {
		h.jsonError(w, http.StatusBadRequest, "invalid body")
		return
	}
	userID := h.getUserID(r) // may be empty — public endpoint
	updated, err := h.Trips.Poll.Vote(r.Context(), slug, service.VoteInput{
		DisplayName: req.DisplayName, Email: req.Email, UserID: userID,
		OptionVotes: req.OptionVotes, Note: req.Note,
	})
	if err != nil {
		h.jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	h.jsonResponse(w, http.StatusOK, map[string]interface{}{
		"poll":    updated,
		"summary": h.Trips.Poll.Summarize(updated),
	})
}

// ---- Receipt OCR ----

func (h *Handler) ReceiptOCRUpload(w http.ResponseWriter, r *http.Request) {
	// Cap the body before parsing. ParseMultipartForm's argument only bounds
	// what is held in memory — it spills the rest to disk — so without this a
	// large upload is unbounded. Mirrors UploadPhoto.
	r.Body = http.MaxBytesReader(w, r.Body, 8<<20)
	if err := r.ParseMultipartForm(8 << 20); err != nil { // #nosec G120 -- body capped by MaxBytesReader above
		h.jsonError(w, http.StatusBadRequest, "invalid multipart form")
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		h.jsonError(w, http.StatusBadRequest, "missing file field")
		return
	}
	defer func() { _ = file.Close() }()
	out, err := h.Trips.ReceiptOCR.FromImageBytes(r.Context(), io.LimitReader(file, 6<<20), header.Header.Get("Content-Type"))
	if err != nil {
		if errors.Is(err, service.ErrReceiptOCRNotConfigured) {
			h.jsonError(w, http.StatusServiceUnavailable, "OCR not configured")
			return
		}
		h.jsonError(w, http.StatusBadGateway, err.Error())
		return
	}
	h.jsonResponse(w, http.StatusOK, out)
}

// ---- AI refine ----

type refineReq struct {
	Prior    *service.Itinerary `json:"prior"`
	Feedback string             `json:"feedback"`
}

func (h *Handler) AIItineraryRefine(w http.ResponseWriter, r *http.Request) {
	var req refineReq
	if err := h.parseJSON(r, &req); err != nil {
		h.jsonError(w, http.StatusBadRequest, "invalid body")
		return
	}
	out, err := h.Trips.AIItinerary.Refine(r.Context(), service.RefinementTurn{
		Prior: req.Prior, Feedback: req.Feedback,
	})
	if err != nil {
		if errors.Is(err, service.ErrAIItineraryNotConfigured) {
			h.jsonError(w, http.StatusServiceUnavailable, "AI not configured")
			return
		}
		h.jsonError(w, http.StatusBadGateway, err.Error())
		return
	}
	h.jsonResponse(w, http.StatusOK, out)
}
