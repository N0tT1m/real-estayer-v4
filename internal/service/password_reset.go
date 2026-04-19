package service

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/realestayer/v4/internal/logctx"
	"github.com/realestayer/v4/internal/mailer"
	"github.com/realestayer/v4/internal/models"
	"github.com/realestayer/v4/internal/repository"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"golang.org/x/crypto/bcrypt"
)

// ResetTokenTTL defines how long a password-reset link is valid.
const ResetTokenTTL = 30 * time.Minute

// PasswordResetService issues reset tokens and consumes them.
//
// The flow:
//  1. User submits email → RequestReset returns a tokenClear to include in a
//     link. Only a SHA-256 hash of the token is stored; we can't reconstruct
//     the link from the DB.
//  2. User clicks link with ?token=clear → CompleteReset validates, rotates
//     the password, and invalidates all sessions.
//
// Nothing here actually emails the user — SendLink is handed the URL so the
// caller can route it to their preferred channel (SMTP, Discord, log line).
type PasswordResetService struct {
	users    *repository.UserRepository
	sessions *repository.SessionRepository
	resets   *repository.PasswordResetRepository
	mailer   *mailer.Mailer
	// hmacKey peppers the token hash. Without it, a DB leak gives an
	// attacker every valid token hash; a pre-computed rainbow table of
	// hex-encoded 32-byte strings is cheap. HMAC forces the attacker to
	// also steal the server secret before any hash can be inverted.
	hmacKey []byte
}

func NewPasswordResetService(users *repository.UserRepository, sessions *repository.SessionRepository, resets *repository.PasswordResetRepository, m *mailer.Mailer, hmacSecret string) *PasswordResetService {
	// Derive a dedicated sub-key so the reset hash can't be trivially
	// replayed against any other HMAC use of the same session secret.
	sum := sha256.Sum256([]byte("real-estayer/password-reset/v1:" + hmacSecret))
	return &PasswordResetService{
		users:    users,
		sessions: sessions,
		resets:   resets,
		mailer:   m,
		hmacKey:  sum[:],
	}
}

// hashToken applies HMAC-SHA-256 with the service's derived key. This is the
// value stored in Mongo; the clear token only lives in the user's inbox.
func (s *PasswordResetService) hashToken(tokenClear string) string {
	mac := hmac.New(sha256.New, s.hmacKey)
	mac.Write([]byte(tokenClear))
	return hex.EncodeToString(mac.Sum(nil))
}

// SendResetEmail formats and dispatches the reset link. Errors are logged so
// the handler can treat the mail step as best-effort (the token is already
// persisted and valid either way).
func (s *PasswordResetService) SendResetEmail(toEmail, link string) {
	if s.mailer == nil || !s.mailer.Configured() {
		slog.Info("password reset issued (mailer not configured)", "email", toEmail, "link", link)
		return
	}
	subject := "Reset your Real-Estayer password"
	text := fmt.Sprintf(
		"Someone requested a password reset for this account.\n\nOpen this link within 30 minutes to choose a new password:\n%s\n\nIf you didn't ask for this, you can ignore this email.",
		link,
	)
	html := fmt.Sprintf(
		`<p>Someone requested a password reset for this account.</p>`+
			`<p><a href="%s" style="background:#2563eb;color:#fff;padding:10px 16px;border-radius:8px;text-decoration:none;display:inline-block">Choose a new password</a></p>`+
			`<p style="color:#6b7280;font-size:12px">Or copy this link: <br><code>%s</code></p>`+
			`<p style="color:#6b7280;font-size:12px">This link is valid for 30 minutes. If you didn't request this, you can ignore this email.</p>`,
		link, link,
	)
	if err := s.mailer.Send(mailer.Message{
		To:      []string{toEmail},
		Subject: subject,
		Text:    text,
		HTML:    html,
	}); err != nil {
		slog.Warn("password reset email failed", "error", err, "email", toEmail)
	}
}

// RequestReset returns (tokenClear, exists). When the email doesn't match any
// account the function returns ("", false, nil) — callers should NOT vary
// their response based on this to avoid account enumeration.
func (s *PasswordResetService) RequestReset(ctx context.Context, email string) (string, bool, error) {
	user, err := s.users.FindByEmail(ctx, email)
	if errors.Is(err, repository.ErrUserNotFound) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}

	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", false, fmt.Errorf("generate reset token: %w", err)
	}
	tokenClear := hex.EncodeToString(raw)
	tokenHash := s.hashToken(tokenClear)

	// Wipe any outstanding resets for this user so only the latest link works.
	if err := s.resets.DeleteForUser(ctx, user.ID); err != nil {
		logctx.From(ctx).Warn("password reset: cleanup of prior tokens failed", "user_id", user.ID.Hex(), "error", err)
	}

	reset := &models.PasswordReset{
		UserID:    user.ID,
		TokenHash: tokenHash,
		ExpiresAt: time.Now().Add(ResetTokenTTL),
	}
	if err := s.resets.Create(ctx, reset); err != nil {
		return "", false, err
	}
	return tokenClear, true, nil
}

// CompleteReset consumes a token and sets a new password. All existing
// sessions for the user are invalidated.
func (s *PasswordResetService) CompleteReset(ctx context.Context, tokenClear, newPassword string) error {
	if err := ValidatePasswordStrength(newPassword); err != nil {
		return err
	}
	tokenHash := s.hashToken(tokenClear)

	reset, err := s.resets.FindByHash(ctx, tokenHash)
	if err != nil {
		if errors.Is(err, repository.ErrResetNotFound) {
			return ErrInvalidCredentials
		}
		return err
	}

	user, err := s.users.FindByID(ctx, reset.UserID)
	if err != nil {
		return err
	}

	hashed, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	if err := s.users.UpdatePassword(ctx, user.ID, string(hashed)); err != nil {
		return err
	}

	if err := s.resets.MarkUsed(ctx, reset.ID); err != nil {
		return err
	}
	if err := s.sessions.DeleteByUserID(ctx, user.ID); err != nil {
		logctx.From(ctx).Warn("password reset: session revoke failed", "user_id", user.ID.Hex(), "error", err)
	}
	return nil
}

// ensure unused imports fire compile errors at build time rather than at
// tool-use time; the blank identifier keeps the linter quiet when callers
// want to reach for primitive without importing it themselves.
var _ = primitive.NilObjectID
