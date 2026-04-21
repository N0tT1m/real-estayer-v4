package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/realestayer/v4/internal/logctx"
	"github.com/realestayer/v4/internal/models"
	"github.com/realestayer/v4/internal/repository"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"golang.org/x/crypto/bcrypt"
)

var (
	ErrInvalidCredentials = errors.New("invalid email or password")
	ErrEmailTaken         = errors.New("email already registered")
	ErrWeakPassword       = errors.New("password does not meet complexity requirements")
	ErrInvalidEmail       = errors.New("invalid email address")
	// Returned by Login when the user has 2FA enabled and no OTP was supplied
	// or it failed to verify. The handler uses this to render the OTP step.
	ErrTOTPRequired       = errors.New("two-factor code required")
	ErrTOTPInvalid        = errors.New("two-factor code invalid")
)

var emailRegex = regexp.MustCompile(`^[^\s@]+@[^\s@]+\.[^\s@]+$`)

// AdminBootstrapChecker is satisfied by *config.Config and used to seed initial admin accounts
// without depending on the config package directly.
type AdminBootstrapChecker interface {
	IsAdminBootstrapEmail(email string) bool
}

// AuthService handles authentication logic
type AuthService struct {
	userRepo       *repository.UserRepository
	sessionRepo    *repository.SessionRepository
	sessionSecret  string
	adminBootstrap AdminBootstrapChecker
}

// NewAuthService creates a new auth service
func NewAuthService(userRepo *repository.UserRepository, sessionRepo *repository.SessionRepository, sessionSecret string, adminBootstrap AdminBootstrapChecker) *AuthService {
	return &AuthService{
		userRepo:       userRepo,
		sessionRepo:    sessionRepo,
		sessionSecret:  sessionSecret,
		adminBootstrap: adminBootstrap,
	}
}

// Register creates a new user account
func (s *AuthService) Register(ctx context.Context, req models.RegisterRequest) (*models.AuthResponse, error) {
	email := strings.ToLower(strings.TrimSpace(req.Email))
	if !emailRegex.MatchString(email) || len(email) > 254 {
		return nil, ErrInvalidEmail
	}
	if err := ValidatePasswordStrength(req.Password); err != nil {
		return nil, err
	}
	name := strings.TrimSpace(req.Name)
	if name == "" || len(name) > 100 {
		return nil, errors.New("invalid name")
	}

	// Check if email already exists. We still return a generic "email taken" error to the caller,
	// but the handler may choose to obscure this for anti-enumeration.
	_, err := s.userRepo.FindByEmail(ctx, email)
	if err == nil {
		return nil, ErrEmailTaken
	}
	if !errors.Is(err, repository.ErrUserNotFound) {
		return nil, err
	}

	// Hash password
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}

	isAdmin := false
	if s.adminBootstrap != nil {
		isAdmin = s.adminBootstrap.IsAdminBootstrapEmail(email)
	}

	user := &models.User{
		Email:        email,
		PasswordHash: string(hashedPassword),
		Name:         name,
		Preferences:  models.NewUserPreferences(),
		IsAdmin:      isAdmin,
	}

	if err := s.userRepo.Create(ctx, user); err != nil {
		if errors.Is(err, repository.ErrEmailExists) {
			return nil, ErrEmailTaken
		}
		return nil, err
	}

	token, err := s.createSession(ctx, user.ID, "", "")
	if err != nil {
		return nil, err
	}

	return &models.AuthResponse{
		Token: token,
		User:  user,
	}, nil
}

// Login authenticates a user and creates a session. If the account has 2FA
// enabled, `totpCode` must contain the 6-digit code from the user's
// authenticator app — otherwise ErrTOTPRequired / ErrTOTPInvalid is returned
// without issuing a session.
//
// Any previous sessions for this user are invalidated when the new one
// issues, so a stolen cookie can't outlive the next login. This is a
// deliberately strict rotation policy; if we ever support concurrent
// sessions we'll soften it.
func (s *AuthService) Login(ctx context.Context, req models.LoginRequest, ipAddress, userAgent, totpCode string) (*models.AuthResponse, error) {
	email := strings.ToLower(strings.TrimSpace(req.Email))
	user, err := s.userRepo.FindByEmail(ctx, email)
	if errors.Is(err, repository.ErrUserNotFound) {
		// Dummy compare to equalise timing with the success path.
		_ = bcrypt.CompareHashAndPassword([]byte("$2a$10$CwTycUXWue0Thq9StjUM0uJ8qLPMZ9OZeyJzKZi7C3xw8N6eUbGrK"), []byte(req.Password))
		return nil, ErrInvalidCredentials
	}
	if err != nil {
		return nil, err
	}
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password)); err != nil {
		return nil, ErrInvalidCredentials
	}

	if user.TOTPEnabled {
		code := strings.TrimSpace(totpCode)
		if code == "" {
			return nil, ErrTOTPRequired
		}
		if !TOTPVerify(user.TOTPSecret, code) {
			return nil, ErrTOTPInvalid
		}
	}

	// Bootstrap self-heal: if the email is listed in ADMIN_BOOTSTRAP_EMAILS but
	// the user's flag is false (e.g. registered before the list existed, or
	// demoted by mistake), promote on login. This is safe because the list is
	// operator-controlled via env.
	if !user.IsAdmin && s.adminBootstrap != nil && s.adminBootstrap.IsAdminBootstrapEmail(user.Email) {
		if err := s.userRepo.UpdateRole(ctx, user.ID, "admin"); err != nil {
			logctx.From(ctx).Warn("admin bootstrap promote failed", "user_id", user.ID.Hex(), "error", err)
		} else {
			user.IsAdmin = true
		}
	}

	// Rotate: issue the new cookie first, then drop every other session for
	// this user. Reversing the order would create a window where the account
	// had no sessions at all, so a failure in Create would silently log the
	// user out.
	token, err := s.createSession(ctx, user.ID, ipAddress, userAgent)
	if err != nil {
		return nil, err
	}
	if err := s.sessionRepo.DeleteByUserIDExceptToken(ctx, user.ID, token); err != nil {
		// Rotation failed but the new session is valid — log so we notice
		// stale-session build-up, don't fail the login.
		logctx.From(ctx).Warn("session rotation cleanup failed", "user_id", user.ID.Hex(), "error", err)
	}
	return &models.AuthResponse{Token: token, User: user}, nil
}

// StartTOTPEnrollment generates a fresh secret and stores it on the user
// (TOTPEnabled stays false until Confirm succeeds). Returns the secret +
// provisioning URI for the QR-code renderer in the template.
func (s *AuthService) StartTOTPEnrollment(ctx context.Context, userIDHex, issuer string) (secret, uri string, err error) {
	oid, err := primitive.ObjectIDFromHex(userIDHex)
	if err != nil {
		return "", "", fmt.Errorf("invalid user ID: %w", err)
	}
	user, err := s.userRepo.FindByID(ctx, oid)
	if err != nil {
		return "", "", err
	}
	secret, err = TOTPSecret()
	if err != nil {
		return "", "", err
	}
	user.TOTPSecret = secret
	user.TOTPEnabled = false
	if err := s.userRepo.Update(ctx, user); err != nil {
		return "", "", err
	}
	return secret, TOTPProvisioningURI(secret, issuer, user.Email), nil
}

// ConfirmTOTP turns the pending secret into an active 2FA factor once the
// user types a valid code from their app.
func (s *AuthService) ConfirmTOTP(ctx context.Context, userIDHex, code string) error {
	oid, err := primitive.ObjectIDFromHex(userIDHex)
	if err != nil {
		return fmt.Errorf("invalid user ID: %w", err)
	}
	user, err := s.userRepo.FindByID(ctx, oid)
	if err != nil {
		return err
	}
	if user.TOTPSecret == "" {
		return errors.New("no pending enrollment")
	}
	if !TOTPVerify(user.TOTPSecret, code) {
		return ErrTOTPInvalid
	}
	user.TOTPEnabled = true
	return s.userRepo.Update(ctx, user)
}

// DisableTOTP removes the 2FA factor after the user proves they still
// control the account (re-authenticate with a current code).
func (s *AuthService) DisableTOTP(ctx context.Context, userIDHex, code string) error {
	oid, err := primitive.ObjectIDFromHex(userIDHex)
	if err != nil {
		return fmt.Errorf("invalid user ID: %w", err)
	}
	user, err := s.userRepo.FindByID(ctx, oid)
	if err != nil {
		return err
	}
	if !user.TOTPEnabled {
		return nil
	}
	if !TOTPVerify(user.TOTPSecret, code) {
		return ErrTOTPInvalid
	}
	user.TOTPEnabled = false
	user.TOTPSecret = ""
	return s.userRepo.Update(ctx, user)
}

// Logout invalidates a session
func (s *AuthService) Logout(ctx context.Context, token string) error {
	return s.sessionRepo.DeleteByToken(ctx, token)
}

// ValidateSession checks if a session token is valid
func (s *AuthService) ValidateSession(ctx context.Context, token string) (*models.User, *models.Session, error) {
	session, err := s.sessionRepo.FindByToken(ctx, token)
	if err != nil {
		return nil, nil, err
	}

	user, err := s.userRepo.FindByID(ctx, session.UserID)
	if err != nil {
		return nil, nil, err
	}

	return user, session, nil
}

// ChangePassword updates a user's password after verifying the current one.
// userIDHex is the caller's user ID (hex-encoded ObjectID) pulled from the session.
func (s *AuthService) ChangePassword(ctx context.Context, userIDHex, currentPassword, newPassword string) error {
	if err := ValidatePasswordStrength(newPassword); err != nil {
		return err
	}

	oid, err := primitive.ObjectIDFromHex(userIDHex)
	if err != nil {
		return fmt.Errorf("invalid user ID: %w", err)
	}

	user, err := s.userRepo.FindByID(ctx, oid)
	if err != nil {
		return err
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(currentPassword)); err != nil {
		return ErrInvalidCredentials
	}

	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return err
	}

	if err := s.userRepo.UpdatePassword(ctx, user.ID, string(hashedPassword)); err != nil {
		return err
	}

	// Invalidate all existing sessions so a stolen cookie can't outlive the password change.
	if err := s.sessionRepo.DeleteByUserID(ctx, user.ID); err != nil {
		logctx.From(ctx).Warn("session revoke after password change failed", "user_id", user.ID.Hex(), "error", err)
	}
	return nil
}

// ValidatePasswordStrength enforces a minimum policy: length 10, upper + lower
// + digit, and rejects obvious low-entropy inputs like "aaaaaaaa1" or
// "password1". We don't try to be a full entropy estimator — that's what
// zxcvbn is for — but this catches the worst offenders without a dependency.
func ValidatePasswordStrength(pw string) error {
	if len(pw) < 10 || len(pw) > 256 {
		return ErrWeakPassword
	}
	var hasLower, hasUpper, hasDigit bool
	unique := make(map[rune]struct{}, len(pw))
	for _, r := range pw {
		unique[r] = struct{}{}
		switch {
		case r >= 'a' && r <= 'z':
			hasLower = true
		case r >= 'A' && r <= 'Z':
			hasUpper = true
		case r >= '0' && r <= '9':
			hasDigit = true
		}
	}
	if !hasLower || !hasUpper || !hasDigit {
		return ErrWeakPassword
	}
	// Require at least 6 distinct characters so "AAAaaa111!" style inputs
	// that technically satisfy the class checks still get rejected.
	if len(unique) < 6 {
		return ErrWeakPassword
	}
	lower := strings.ToLower(pw)
	for _, banned := range commonWeakPasswords {
		if strings.Contains(lower, banned) {
			return ErrWeakPassword
		}
	}
	return nil
}

// commonWeakPasswords is a short blocklist of substrings that make a
// password trivially guessable regardless of class composition. Kept small
// on purpose — a real deployment should front this with a rockyou-backed
// list (or zxcvbn) before rollout.
var commonWeakPasswords = []string{
	"password",
	"passw0rd",
	"qwerty",
	"letmein",
	"iloveyou",
	"welcome",
	"admin123",
	"12345678",
	"realestayer",
}

// createSession generates a new session token
func (s *AuthService) createSession(ctx context.Context, userID interface{}, ipAddress, userAgent string) (string, error) {
	token, err := generateToken(32)
	if err != nil {
		return "", err
	}

	var oid primitive.ObjectID
	switch v := userID.(type) {
	case primitive.ObjectID:
		oid = v
	case string:
		var err error
		oid, err = primitive.ObjectIDFromHex(v)
		if err != nil {
			return "", fmt.Errorf("invalid user ID: %w", err)
		}
	default:
		return "", fmt.Errorf("unsupported user ID type: %T", userID)
	}

	session := &models.Session{
		UserID:    oid,
		Token:     token,
		ExpiresAt: time.Now().Add(7 * 24 * time.Hour),
		IPAddress: ipAddress,
		UserAgent: userAgent,
	}

	if err := s.sessionRepo.Create(ctx, session); err != nil {
		return "", err
	}

	return token, nil
}

func generateToken(length int) (string, error) {
	bytes := make([]byte, length)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}
