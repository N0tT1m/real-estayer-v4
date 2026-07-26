package service

import (
	"context"
	"errors"
	"log/slog"
	"strings"

	"github.com/realestayer/v4/internal/crypto"
	"github.com/realestayer/v4/internal/models"
	"github.com/realestayer/v4/internal/repository"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

var errInvalidDiscordWebhook = errors.New("discord webhook must be an https://discord.com/api/webhooks/... URL")

// isDiscordWebhook validates that a string is plausibly a Discord webhook URL.
// Enforcing the host prevents the app from becoming an SSRF conduit — we only
// POST to discord.com. Also guards against accidentally storing non-URL junk.
func isDiscordWebhook(u string) bool {
	u = strings.TrimSpace(u)
	return strings.HasPrefix(u, "https://discord.com/api/webhooks/") ||
		strings.HasPrefix(u, "https://discordapp.com/api/webhooks/") ||
		strings.HasPrefix(u, "https://canary.discord.com/api/webhooks/") ||
		strings.HasPrefix(u, "https://ptb.discord.com/api/webhooks/")
}

// ErrInvalidDiscordWebhook lets callers recognize the validation failure.
func ErrInvalidDiscordWebhook() error { return errInvalidDiscordWebhook }

// IsDiscordWebhook exposes the host check to callers that are about to POST to
// a stored webhook. Validating on save is not sufficient on its own: a value
// written before the check existed, or edited directly in the database, would
// otherwise turn this app into an SSRF conduit. Re-check at send time.
func IsDiscordWebhook(u string) bool { return isDiscordWebhook(u) }

// UserService handles user-related business logic
type UserService struct {
	userRepo *repository.UserRepository
	cipher   *crypto.FieldCipher
}

// NewUserService creates a new user service. Pass a nil cipher in tests or
// if you don't want field encryption — the service will fall through to
// plain-text storage and log a single warning.
func NewUserService(userRepo *repository.UserRepository, cipher *crypto.FieldCipher) *UserService {
	if cipher == nil {
		cipher, _ = crypto.NewFieldCipher("")
	}
	if !cipher.Enabled() {
		slog.Warn("user identity fields are stored plain-text — set FIELD_ENCRYPTION_KEY to enable AES-GCM at rest")
	}
	return &UserService{userRepo: userRepo, cipher: cipher}
}

// GetByID retrieves a user by ID
func (s *UserService) GetByID(ctx context.Context, id string) (*models.User, error) {
	objID, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return nil, repository.ErrUserNotFound
	}
	return s.userRepo.FindByID(ctx, objID)
}

// Update updates a user's profile
func (s *UserService) Update(ctx context.Context, id string, req models.UpdateUserRequest) (*models.User, error) {
	objID, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return nil, repository.ErrUserNotFound
	}

	user, err := s.userRepo.FindByID(ctx, objID)
	if err != nil {
		return nil, err
	}

	if req.Name != "" {
		user.Name = req.Name
	}
	if req.AvatarURL != "" {
		user.AvatarURL = req.AvatarURL
	}
	if req.Preferences != nil {
		user.Preferences = *req.Preferences
	}

	if err := s.userRepo.Update(ctx, user); err != nil {
		return nil, err
	}

	return user, nil
}

// UpdateIdentity stores the user's traveler-identity block. All fields are
// optional and the caller is expected to have already done light sanity
// checks (e.g. passport_number length) on the input. Sensitive fields are
// encrypted before persisting.
func (s *UserService) UpdateIdentity(ctx context.Context, id string, identity models.TravelerIdentity) (*models.User, error) {
	objID, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return nil, repository.ErrUserNotFound
	}
	user, err := s.userRepo.FindByID(ctx, objID)
	if err != nil {
		return nil, err
	}
	user.Identity = s.encryptIdentity(identity)
	if err := s.userRepo.Update(ctx, user); err != nil {
		return nil, err
	}
	// Return the plain-text form to the caller so the UI can round-trip.
	user.Identity = identity
	return user, nil
}

// DecryptIdentity should be called by anything reading user.Identity back
// out of storage. Idempotent when fields aren't tagged as ciphertext.
func (s *UserService) DecryptIdentity(u *models.User) {
	if u == nil {
		return
	}
	u.Identity.PassportNumber = s.decryptField(u.Identity.PassportNumber)
	u.Identity.KnownTravelerNo = s.decryptField(u.Identity.KnownTravelerNo)
	u.Identity.RedressNo = s.decryptField(u.Identity.RedressNo)
	u.Identity.GlobalEntryID = s.decryptField(u.Identity.GlobalEntryID)
	for i, la := range u.Identity.LoyaltyAccounts {
		u.Identity.LoyaltyAccounts[i].Number = s.decryptField(la.Number)
	}
}

func (s *UserService) encryptIdentity(in models.TravelerIdentity) models.TravelerIdentity {
	out := in
	out.PassportNumber = s.encryptField(out.PassportNumber)
	out.KnownTravelerNo = s.encryptField(out.KnownTravelerNo)
	out.RedressNo = s.encryptField(out.RedressNo)
	out.GlobalEntryID = s.encryptField(out.GlobalEntryID)
	out.LoyaltyAccounts = make([]models.LoyaltyAccount, len(in.LoyaltyAccounts))
	for i, la := range in.LoyaltyAccounts {
		la.Number = s.encryptField(la.Number)
		out.LoyaltyAccounts[i] = la
	}
	return out
}

func (s *UserService) encryptField(v string) string {
	if v == "" {
		return ""
	}
	ct, err := s.cipher.Encrypt(v)
	if err != nil {
		slog.Warn("encryptField failed; storing plain-text", "error", err)
		return v
	}
	return ct
}

func (s *UserService) decryptField(v string) string {
	if v == "" {
		return ""
	}
	pt, err := s.cipher.Decrypt(v)
	if err != nil {
		slog.Warn("decryptField failed; returning raw", "error", err)
		return v
	}
	return pt
}

// UpdateNotifications replaces the user's notification preferences. A webhook
// URL must be https://discord.com/api/webhooks/... — anything else is
// rejected so we don't become a proxy for arbitrary outbound POSTs.
func (s *UserService) UpdateNotifications(ctx context.Context, id string, prefs models.NotificationSettings) (*models.User, error) {
	objID, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return nil, repository.ErrUserNotFound
	}
	if prefs.DiscordWebhook != "" {
		if !isDiscordWebhook(prefs.DiscordWebhook) {
			return nil, errInvalidDiscordWebhook
		}
	}
	user, err := s.userRepo.FindByID(ctx, objID)
	if err != nil {
		return nil, err
	}
	user.Preferences.Notifications = prefs
	if err := s.userRepo.Update(ctx, user); err != nil {
		return nil, err
	}
	return user, nil
}

// List returns paginated users (admin only)
func (s *UserService) List(ctx context.Context, page, limit int) ([]models.User, int64, error) {
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 20
	}
	return s.userRepo.List(ctx, page, limit)
}

// GetStats returns user statistics
func (s *UserService) GetStats(ctx context.Context) (map[string]interface{}, error) {
	count, err := s.userRepo.Count(ctx)
	if err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"total_users": count,
	}, nil
}

// UpdateRole updates a user's role (admin only)
func (s *UserService) UpdateRole(ctx context.Context, id string, role string) error {
	objID, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return repository.ErrUserNotFound
	}
	return s.userRepo.UpdateRole(ctx, objID, role)
}
