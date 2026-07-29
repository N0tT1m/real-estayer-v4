package service

import (
	"testing"
	"time"

	"github.com/realestayer/v4/internal/dbtest"
	"github.com/realestayer/v4/internal/models"
	"github.com/realestayer/v4/internal/repository"
)

// enrolledUser returns an auth service plus a user with 2FA already active.
func enrolledUser(t *testing.T) (*AuthService, *repository.UserRepository, *models.User) {
	t.Helper()

	db := dbtest.New(t)
	ctx := dbtest.Context(t)

	users := repository.NewUserRepository(db)
	sessions := repository.NewSessionRepository(db)
	auth := NewAuthService(users, sessions, "test-secret-at-least-32-chars-long!!", nil)

	u := &models.User{
		Email:        "totp@example.com",
		PasswordHash: "x",
		Name:         "TOTP User",
		Preferences:  models.NewUserPreferences(),
	}
	if err := users.Create(ctx, u); err != nil {
		t.Fatalf("create user: %v", err)
	}

	// Enroll and activate, the way a real user would.
	secret, _, err := auth.StartTOTPEnrollment(ctx, u.ID.Hex(), "Real-Estayer")
	if err != nil {
		t.Fatalf("StartTOTPEnrollment: %v", err)
	}
	if err := auth.ConfirmTOTP(ctx, u.ID.Hex(), currentTOTP(t, secret)); err != nil {
		t.Fatalf("ConfirmTOTP: %v", err)
	}

	got, err := users.FindByID(ctx, u.ID)
	if err != nil {
		t.Fatalf("reload user: %v", err)
	}
	if !got.TOTPEnabled {
		t.Fatal("precondition failed: 2FA should be active")
	}
	return auth, users, got
}

// currentTOTP computes the code the user's authenticator would be showing.
func currentTOTP(t *testing.T, secret string) string {
	t.Helper()
	code := hotp(secret, uint64(time.Now().Unix()/30))
	if code == "" {
		t.Fatalf("could not derive a current TOTP code from %q", secret)
	}
	return code
}

// Re-enrolling must not silently drop the existing factor.
//
// StartTOTPEnrollment used to set TOTPEnabled=false unconditionally, so anyone
// holding a hijacked session could POST /users/me/totp/start and turn 2FA off
// without ever proving possession of the enrolled device — bypassing the code
// check DisableTOTP deliberately enforces.
func TestStartEnrollmentDoesNotDisableActive2FA(t *testing.T) {
	auth, users, u := enrolledUser(t)
	ctx := dbtest.Context(t)

	if _, _, err := auth.StartTOTPEnrollment(ctx, u.ID.Hex(), "Real-Estayer"); err == nil {
		t.Error("re-enrollment while 2FA is active should be refused")
	}

	after, err := users.FindByID(ctx, u.ID)
	if err != nil {
		t.Fatalf("reload user: %v", err)
	}
	if !after.TOTPEnabled {
		t.Error("2FA was disabled by StartTOTPEnrollment without any code check")
	}
	if after.TOTPSecret != u.TOTPSecret {
		t.Error("the active TOTP secret was overwritten without a code check")
	}
}

// Disabling still has to work when the user does prove possession.
func TestDisableTOTPWithValidCodeStillWorks(t *testing.T) {
	auth, users, u := enrolledUser(t)
	ctx := dbtest.Context(t)

	if err := auth.DisableTOTP(ctx, u.ID.Hex(), currentTOTP(t, u.TOTPSecret)); err != nil {
		t.Fatalf("DisableTOTP with a valid code: %v", err)
	}

	after, err := users.FindByID(ctx, u.ID)
	if err != nil {
		t.Fatalf("reload user: %v", err)
	}
	if after.TOTPEnabled || after.TOTPSecret != "" {
		t.Error("valid-code disable did not clear the factor")
	}

	// With the factor gone, enrollment must be possible again.
	if _, _, err := auth.StartTOTPEnrollment(ctx, u.ID.Hex(), "Real-Estayer"); err != nil {
		t.Errorf("re-enrollment after a clean disable should be allowed: %v", err)
	}
}

// A wrong code must not disable the factor.
func TestDisableTOTPRejectsBadCode(t *testing.T) {
	auth, users, u := enrolledUser(t)
	ctx := dbtest.Context(t)

	if err := auth.DisableTOTP(ctx, u.ID.Hex(), "000000"); err == nil {
		t.Error("DisableTOTP accepted an incorrect code")
	}
	after, err := users.FindByID(ctx, u.ID)
	if err != nil {
		t.Fatalf("reload user: %v", err)
	}
	if !after.TOTPEnabled {
		t.Error("an incorrect code disabled 2FA")
	}
}
