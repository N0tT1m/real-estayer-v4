package service

import (
	"context"
	"crypto/rand"
	"encoding/base32"
	"errors"
	"strings"
	"time"

	"github.com/realestayer/v4/internal/models"
	"github.com/realestayer/v4/internal/repository"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// randomSlug produces a URL-safe, lowercase slug of about the requested length.
func randomSlug(n int) (string, error) {
	raw := make([]byte, n)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	enc := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(raw)
	enc = strings.ToLower(enc)
	if len(enc) > n {
		enc = enc[:n]
	}
	return enc, nil
}

var ErrTripAccessDenied = errors.New("access to trip denied")

// TripService handles trip-related business logic
type TripService struct {
	tripRepo *repository.TripRepository
}

// NewTripService creates a new trip service
func NewTripService(tripRepo *repository.TripRepository) *TripService {
	return &TripService{
		tripRepo: tripRepo,
	}
}

// Create creates a new trip
func (s *TripService) Create(ctx context.Context, userID string, req models.CreateTripRequest) (*models.Trip, error) {
	objID, err := primitive.ObjectIDFromHex(userID)
	if err != nil {
		return nil, errors.New("invalid user ID")
	}

	trip := &models.Trip{
		UserID:       objID,
		Name:         req.Name,
		Description:  req.Description,
		Status:       models.TripStatusPlanning,
		StartDate:    req.StartDate,
		EndDate:      req.EndDate,
		Destinations: req.Destinations,
		Items:        []models.TripItem{},
		TotalBudget: models.TripBudget{
			Estimated: req.Budget,
			Currency:  req.Currency,
		},
	}

	if trip.TotalBudget.Currency == "" {
		trip.TotalBudget.Currency = "USD"
	}

	if err := s.tripRepo.Create(ctx, trip); err != nil {
		return nil, err
	}

	return trip, nil
}

// GetByID retrieves a trip by ID (with access check)
func (s *TripService) GetByID(ctx context.Context, userID, tripID string) (*models.Trip, error) {
	objID, err := primitive.ObjectIDFromHex(tripID)
	if err != nil {
		return nil, repository.ErrTripNotFound
	}

	trip, err := s.tripRepo.FindByID(ctx, objID)
	if err != nil {
		return nil, err
	}

	// Check access
	if !s.hasAccess(userID, trip) {
		return nil, ErrTripAccessDenied
	}

	return trip, nil
}

// GetUserTrips returns all trips for a user
func (s *TripService) GetUserTrips(ctx context.Context, userID string, page, limit int) ([]models.Trip, int64, error) {
	objID, err := primitive.ObjectIDFromHex(userID)
	if err != nil {
		return nil, 0, errors.New("invalid user ID")
	}

	return s.tripRepo.FindByUserID(ctx, objID, page, limit)
}

// Update updates a trip
func (s *TripService) Update(ctx context.Context, userID, tripID string, req models.UpdateTripRequest) (*models.Trip, error) {
	trip, err := s.GetByID(ctx, userID, tripID)
	if err != nil {
		return nil, err
	}

	// Only owner can update
	if trip.UserID.Hex() != userID {
		return nil, ErrTripAccessDenied
	}

	if req.Name != "" {
		trip.Name = req.Name
	}
	if req.Description != "" {
		trip.Description = req.Description
	}
	if req.Status != "" {
		trip.Status = req.Status
	}
	if req.StartDate != nil {
		trip.StartDate = *req.StartDate
	}
	if req.EndDate != nil {
		trip.EndDate = *req.EndDate
	}
	if req.Destinations != nil {
		trip.Destinations = req.Destinations
	}
	if req.Budget != nil {
		trip.TotalBudget.Estimated = *req.Budget
	}

	if err := s.tripRepo.Update(ctx, trip); err != nil {
		return nil, err
	}

	return trip, nil
}

// Delete removes a trip
func (s *TripService) Delete(ctx context.Context, userID, tripID string) error {
	trip, err := s.GetByID(ctx, userID, tripID)
	if err != nil {
		return err
	}

	// Only owner can delete
	if trip.UserID.Hex() != userID {
		return ErrTripAccessDenied
	}

	return s.tripRepo.Delete(ctx, trip.ID)
}

// AddItem adds an item to a trip
func (s *TripService) AddItem(ctx context.Context, userID, tripID string, req models.AddTripItemRequest) (*models.Trip, error) {
	trip, err := s.GetByID(ctx, userID, tripID)
	if err != nil {
		return nil, err
	}

	item := models.TripItem{
		Type:        req.Type,
		ReferenceID: req.ReferenceID,
		Provider:    req.Provider,
		Title:       req.Title,
		Status:      "planned",
		Details:     req.Details,
		Price:       req.Price,
		StartTime:   req.StartTime,
		EndTime:     req.EndTime,
		Notes:       req.Notes,
	}

	if err := s.tripRepo.AddItem(ctx, trip.ID, item); err != nil {
		return nil, err
	}

	// Return updated trip
	return s.tripRepo.FindByID(ctx, trip.ID)
}

// RemoveItem removes an item from a trip
func (s *TripService) RemoveItem(ctx context.Context, userID, tripID, itemID string) error {
	trip, err := s.GetByID(ctx, userID, tripID)
	if err != nil {
		return err
	}

	itemObjID, err := primitive.ObjectIDFromHex(itemID)
	if err != nil {
		return errors.New("invalid item ID")
	}

	return s.tripRepo.RemoveItem(ctx, trip.ID, itemObjID)
}

// ShareWithUser shares a trip with another user
func (s *TripService) ShareWithUser(ctx context.Context, ownerID, tripID, shareWithUserID string) error {
	trip, err := s.GetByID(ctx, ownerID, tripID)
	if err != nil {
		return err
	}

	// Only owner can share
	if trip.UserID.Hex() != ownerID {
		return ErrTripAccessDenied
	}

	shareWithObjID, err := primitive.ObjectIDFromHex(shareWithUserID)
	if err != nil {
		return errors.New("invalid user ID")
	}

	return s.tripRepo.ShareWithUser(ctx, trip.ID, shareWithObjID)
}

// EnableSharing generates (or reuses) a share slug for the given trip. Only
// the owner can enable sharing.
func (s *TripService) EnableSharing(ctx context.Context, userID, tripID string) (string, error) {
	trip, err := s.GetByID(ctx, userID, tripID)
	if err != nil {
		return "", err
	}
	if trip.UserID.Hex() != userID {
		return "", ErrTripAccessDenied
	}
	if trip.ShareSlug != "" {
		return trip.ShareSlug, nil
	}
	slug, err := randomSlug(16)
	if err != nil {
		return "", err
	}
	if err := s.tripRepo.SetShareSlug(ctx, trip.ID, slug); err != nil {
		return "", err
	}
	return slug, nil
}

// DisableSharing clears the share slug so the public link stops working.
func (s *TripService) DisableSharing(ctx context.Context, userID, tripID string) error {
	trip, err := s.GetByID(ctx, userID, tripID)
	if err != nil {
		return err
	}
	if trip.UserID.Hex() != userID {
		return ErrTripAccessDenied
	}
	return s.tripRepo.SetShareSlug(ctx, trip.ID, "")
}

// GetBySlug returns a trip by its public share slug. No auth — caller is
// responsible for knowing the slug.
func (s *TripService) GetBySlug(ctx context.Context, slug string) (*models.Trip, error) {
	return s.tripRepo.FindBySlug(ctx, slug)
}

// GetUpcoming returns upcoming trips for a user
func (s *TripService) GetUpcoming(ctx context.Context, userID string, limit int) ([]models.Trip, error) {
	objID, err := primitive.ObjectIDFromHex(userID)
	if err != nil {
		return nil, errors.New("invalid user ID")
	}

	return s.tripRepo.GetUpcoming(ctx, objID, limit)
}

// hasAccess checks if a user has read access to a trip. Owners, legacy
// shared_with users, and named collaborators (any role) can view.
func (s *TripService) hasAccess(userID string, trip *models.Trip) bool {
	if trip == nil {
		return false
	}
	if trip.UserID.Hex() == userID {
		return true
	}
	for _, sharedID := range trip.SharedWith {
		if sharedID.Hex() == userID {
			return true
		}
	}
	for _, c := range trip.Collaborators {
		if c.UserID.Hex() == userID {
			return true
		}
	}
	return false
}

// CanEdit reports whether the given user can mutate the trip. Owner and any
// collaborator with the editor role qualify.
func (s *TripService) CanEdit(userID string, trip *models.Trip) bool {
	if trip == nil {
		return false
	}
	if trip.UserID.Hex() == userID {
		return true
	}
	for _, c := range trip.Collaborators {
		if c.UserID.Hex() == userID && c.Role == models.TripRoleEditor {
			return true
		}
	}
	return false
}

// ReorderItems rewrites the item order for a trip. The provided slice must be
// a permutation of the trip's existing items (same IDs, same count).
func (s *TripService) ReorderItems(ctx context.Context, userID, tripID string, orderedIDs []string) error {
	trip, err := s.GetByID(ctx, userID, tripID)
	if err != nil {
		return err
	}
	if !s.CanEdit(userID, trip) {
		return ErrTripAccessDenied
	}
	if len(orderedIDs) != len(trip.Items) {
		return errors.New("reorder length mismatch")
	}
	byID := make(map[string]models.TripItem, len(trip.Items))
	for _, it := range trip.Items {
		byID[it.ID.Hex()] = it
	}
	out := make([]models.TripItem, 0, len(orderedIDs))
	for _, id := range orderedIDs {
		it, ok := byID[id]
		if !ok {
			return errors.New("reorder referenced unknown item")
		}
		out = append(out, it)
	}
	return s.tripRepo.ReorderItems(ctx, trip.ID, out)
}

// AddCollaboratorByEmail invites a registered user to collaborate on a trip.
// Only the trip's owner can invite. Email must match an existing account —
// we deliberately don't create user accounts off invitations here.
func (s *TripService) AddCollaboratorByEmail(ctx context.Context, ownerID, tripID, email string, role models.TripRole, users *repository.UserRepository) (*models.TripCollaborator, error) {
	if role != models.TripRoleEditor && role != models.TripRoleViewer {
		return nil, errors.New("invalid role")
	}
	trip, err := s.GetByID(ctx, ownerID, tripID)
	if err != nil {
		return nil, err
	}
	if trip.UserID.Hex() != ownerID {
		return nil, ErrTripAccessDenied
	}
	u, err := users.FindByEmail(ctx, strings.ToLower(strings.TrimSpace(email)))
	if err != nil {
		return nil, errors.New("no account found for that email")
	}
	if u.ID == trip.UserID {
		return nil, errors.New("you already own this trip")
	}
	c := models.TripCollaborator{
		UserID: u.ID,
		Email:  u.Email,
		Name:   u.Name,
		Role:   role,
	}
	if err := s.tripRepo.AddCollaborator(ctx, trip.ID, c); err != nil {
		return nil, err
	}
	return &c, nil
}

// RemoveCollaborator drops a collaborator (owner-only).
func (s *TripService) RemoveCollaborator(ctx context.Context, ownerID, tripID, userIDHex string) error {
	trip, err := s.GetByID(ctx, ownerID, tripID)
	if err != nil {
		return err
	}
	if trip.UserID.Hex() != ownerID {
		return ErrTripAccessDenied
	}
	target, err := primitive.ObjectIDFromHex(userIDHex)
	if err != nil {
		return errors.New("invalid user id")
	}
	return s.tripRepo.RemoveCollaborator(ctx, trip.ID, target)
}

// Clone duplicates a trip, shifting all dates by the same delta so the copy
// starts on newStart. Items keep their relative ordering and durations.
// Expenses/comments/journal/reviews are NOT copied — each copy is a fresh
// plan.
func (s *TripService) Clone(ctx context.Context, userID, tripID string, newStart time.Time, newName string) (*models.Trip, error) {
	orig, err := s.GetByID(ctx, userID, tripID)
	if err != nil {
		return nil, err
	}
	delta := newStart.Sub(orig.StartDate)

	copyItems := make([]models.TripItem, len(orig.Items))
	for i, it := range orig.Items {
		it.ID = primitive.NewObjectID()
		if it.StartTime != nil {
			shifted := it.StartTime.Add(delta)
			it.StartTime = &shifted
		}
		if it.EndTime != nil {
			shifted := it.EndTime.Add(delta)
			it.EndTime = &shifted
		}
		copyItems[i] = it
	}

	dests := make([]models.TripDestination, len(orig.Destinations))
	for i, d := range orig.Destinations {
		d.ArrivalDate = d.ArrivalDate.Add(delta)
		d.DepartureDate = d.DepartureDate.Add(delta)
		dests[i] = d
	}

	name := strings.TrimSpace(newName)
	if name == "" {
		name = orig.Name + " (copy)"
	}

	copy := &models.Trip{
		UserID:       orig.UserID,
		Name:         name,
		Description:  orig.Description,
		Status:       models.TripStatusPlanning,
		StartDate:    newStart,
		EndDate:      orig.EndDate.Add(delta),
		Destinations: dests,
		Items:        copyItems,
		TotalBudget:  orig.TotalBudget,
		PackingList:  clonePackingList(orig.PackingList),
		Checklist:    cloneChecklist(orig.Checklist),
	}
	if err := s.tripRepo.Create(ctx, copy); err != nil {
		return nil, err
	}
	return copy, nil
}

func clonePackingList(in []models.PackingItem) []models.PackingItem {
	out := make([]models.PackingItem, len(in))
	for i, p := range in {
		p.ID = primitive.NewObjectID()
		p.Packed = false
		out[i] = p
	}
	return out
}

func cloneChecklist(in []models.ChecklistItem) []models.ChecklistItem {
	out := make([]models.ChecklistItem, len(in))
	for i, c := range in {
		c.ID = primitive.NewObjectID()
		c.Done = false
		out[i] = c
	}
	return out
}

// SetPackingList replaces the list atomically (for drag-reorder / bulk edit).
func (s *TripService) SetPackingList(ctx context.Context, userID, tripID string, items []models.PackingItem) error {
	trip, err := s.GetByID(ctx, userID, tripID)
	if err != nil {
		return err
	}
	if !s.CanEdit(userID, trip) {
		return ErrTripAccessDenied
	}
	for i := range items {
		if items[i].ID.IsZero() {
			items[i].ID = primitive.NewObjectID()
		}
		if items[i].CreatedAt.IsZero() {
			items[i].CreatedAt = time.Now()
		}
		items[i].TripID = trip.ID
	}
	return s.tripRepo.SetPackingList(ctx, trip.ID, items)
}

// SetChecklist — same idea as SetPackingList.
func (s *TripService) SetChecklist(ctx context.Context, userID, tripID string, items []models.ChecklistItem) error {
	trip, err := s.GetByID(ctx, userID, tripID)
	if err != nil {
		return err
	}
	if !s.CanEdit(userID, trip) {
		return ErrTripAccessDenied
	}
	for i := range items {
		if items[i].ID.IsZero() {
			items[i].ID = primitive.NewObjectID()
		}
		if items[i].CreatedAt.IsZero() {
			items[i].CreatedAt = time.Now()
		}
		items[i].TripID = trip.ID
	}
	return s.tripRepo.SetChecklist(ctx, trip.ID, items)
}
