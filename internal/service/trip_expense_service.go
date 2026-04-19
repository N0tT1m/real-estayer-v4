package service

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/realestayer/v4/internal/models"
	"github.com/realestayer/v4/internal/repository"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// TripExpenseService layers permission checks and the budget rollup on top of
// the raw expense repo.
type TripExpenseService struct {
	trips    *TripService
	expenses *repository.TripExpenseRepository
	users    *repository.UserRepository
}

func NewTripExpenseService(trips *TripService, expenses *repository.TripExpenseRepository, users *repository.UserRepository) *TripExpenseService {
	return &TripExpenseService{trips: trips, expenses: expenses, users: users}
}

// List returns expenses for a trip the user has access to.
func (s *TripExpenseService) List(ctx context.Context, userID, tripID string) ([]models.TripExpense, error) {
	trip, err := s.trips.GetByID(ctx, userID, tripID)
	if err != nil {
		return nil, err
	}
	return s.expenses.ListForTrip(ctx, trip.ID)
}

// CreateExpenseInput is the handler-facing payload for adding an expense.
type CreateExpenseInput struct {
	Description string
	Category    models.TripItemType
	Amount      float64
	Currency    string
	SpentAt     *time.Time
	SplitWith   []string // hex user IDs
}

func (s *TripExpenseService) Create(ctx context.Context, userID, tripID string, in CreateExpenseInput) (*models.TripExpense, error) {
	trip, err := s.trips.GetByID(ctx, userID, tripID)
	if err != nil {
		return nil, err
	}
	if !s.trips.CanEdit(userID, trip) {
		return nil, ErrTripAccessDenied
	}
	if strings.TrimSpace(in.Description) == "" {
		return nil, errors.New("description is required")
	}
	if in.Amount <= 0 {
		return nil, errors.New("amount must be positive")
	}
	payerID, err := primitive.ObjectIDFromHex(userID)
	if err != nil {
		return nil, errors.New("invalid user id")
	}

	splitWith := make([]primitive.ObjectID, 0, len(in.SplitWith))
	for _, h := range in.SplitWith {
		oid, err := primitive.ObjectIDFromHex(h)
		if err != nil {
			continue
		}
		splitWith = append(splitWith, oid)
	}

	payerName := ""
	if payer, err := s.users.FindByID(ctx, payerID); err == nil && payer != nil {
		payerName = payer.Name
	}

	when := time.Now()
	if in.SpentAt != nil {
		when = *in.SpentAt
	}
	currency := in.Currency
	if currency == "" {
		currency = trip.TotalBudget.Currency
		if currency == "" {
			currency = "USD"
		}
	}

	exp := &models.TripExpense{
		TripID:      trip.ID,
		PaidBy:      payerID,
		PaidByName:  payerName,
		Description: strings.TrimSpace(in.Description),
		Category:    in.Category,
		Amount:      in.Amount,
		Currency:    currency,
		SplitWith:   splitWith,
		SpentAt:     when,
	}
	if err := s.expenses.Create(ctx, exp); err != nil {
		return nil, err
	}
	return exp, nil
}

func (s *TripExpenseService) Delete(ctx context.Context, userID, tripID, expenseID string) error {
	trip, err := s.trips.GetByID(ctx, userID, tripID)
	if err != nil {
		return err
	}
	if !s.trips.CanEdit(userID, trip) {
		return ErrTripAccessDenied
	}
	id, err := primitive.ObjectIDFromHex(expenseID)
	if err != nil {
		return errors.New("invalid expense id")
	}
	return s.expenses.Delete(ctx, id)
}

// Summary returns a planned-vs-actual rollup by TripItemType. Planned is the
// sum of item prices (grouped by item type), actual is the sum of recorded
// expenses (grouped by category). All amounts are reported in the trip's
// configured currency — no conversion is applied yet; expenses in other
// currencies are added verbatim (callers should surface this limitation).
func (s *TripExpenseService) Summary(ctx context.Context, userID, tripID string) (*models.TripBudgetSummary, []models.TripExpense, error) {
	trip, err := s.trips.GetByID(ctx, userID, tripID)
	if err != nil {
		return nil, nil, err
	}
	expenses, err := s.expenses.ListForTrip(ctx, trip.ID)
	if err != nil {
		return nil, nil, err
	}

	currency := trip.TotalBudget.Currency
	if currency == "" {
		currency = "USD"
	}
	summary := &models.TripBudgetSummary{
		Estimated:  trip.TotalBudget.Estimated,
		Currency:   currency,
		ByCategory: map[string]models.BudgetLine{},
	}
	for _, it := range trip.Items {
		if it.Price == nil {
			continue
		}
		cat := string(it.Type)
		line := summary.ByCategory[cat]
		line.Planned += it.Price.Amount
		summary.ByCategory[cat] = line
		summary.PlannedTotal += it.Price.Amount
	}
	for _, e := range expenses {
		cat := string(e.Category)
		if cat == "" {
			cat = "other"
		}
		line := summary.ByCategory[cat]
		line.Actual += e.Amount
		summary.ByCategory[cat] = line
		summary.SpentTotal += e.Amount
	}
	return summary, expenses, nil
}

// SettleUp computes a naive net balance per payer for group trips: amount paid
// minus amount owed if splits are equal-share. Positive means the user is
// owed money; negative means they owe.
type Balance struct {
	UserID primitive.ObjectID `json:"user_id"`
	Name   string             `json:"name,omitempty"`
	Net    float64            `json:"net"`
}

func (s *TripExpenseService) SettleUp(ctx context.Context, userID, tripID string) ([]Balance, string, error) {
	trip, err := s.trips.GetByID(ctx, userID, tripID)
	if err != nil {
		return nil, "", err
	}
	expenses, err := s.expenses.ListForTrip(ctx, trip.ID)
	if err != nil {
		return nil, "", err
	}

	nameFor := map[primitive.ObjectID]string{}
	net := map[primitive.ObjectID]float64{}
	for _, c := range trip.Collaborators {
		nameFor[c.UserID] = c.Name
		net[c.UserID] = 0
	}
	nameFor[trip.UserID] = ""
	net[trip.UserID] = 0

	for _, e := range expenses {
		participants := []primitive.ObjectID{e.PaidBy}
		participants = append(participants, e.SplitWith...)
		if len(participants) == 0 {
			participants = []primitive.ObjectID{e.PaidBy}
		}
		share := e.Amount / float64(len(participants))
		net[e.PaidBy] += e.Amount
		for _, p := range participants {
			net[p] -= share
		}
		if e.PaidByName != "" {
			nameFor[e.PaidBy] = e.PaidByName
		}
	}

	out := make([]Balance, 0, len(net))
	for uid, n := range net {
		out = append(out, Balance{UserID: uid, Name: nameFor[uid], Net: round2(n)})
	}
	currency := trip.TotalBudget.Currency
	if currency == "" {
		currency = "USD"
	}
	return out, currency, nil
}

func round2(f float64) float64 {
	return float64(int64(f*100+0.5)) / 100
}
