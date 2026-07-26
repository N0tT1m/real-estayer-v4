package models

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// User represents a registered user
type User struct {
	ID           primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	Email        string             `bson:"email" json:"email"`
	PasswordHash string             `bson:"password_hash" json:"-"`
	Name         string             `bson:"name" json:"name"`
	AvatarURL    string             `bson:"avatar_url,omitempty" json:"avatar_url,omitempty"`
	IsAdmin      bool               `bson:"is_admin" json:"is_admin"`
	TOTPSecret   string             `bson:"totp_secret,omitempty" json:"-"`                       // set during enrol
	TOTPEnabled  bool               `bson:"totp_enabled,omitempty" json:"totp_enabled,omitempty"` // true once user verifies a code
	Identity     TravelerIdentity   `bson:"identity,omitempty" json:"identity,omitempty"`
	Preferences  UserPreferences    `bson:"preferences" json:"preferences"`
	CreatedAt    time.Time          `bson:"created_at" json:"created_at"`
	UpdatedAt    time.Time          `bson:"updated_at" json:"updated_at"`
}

// TravelerIdentity stores the stuff airlines and immigration forms keep
// asking for. Every field is optional; we never verify documents.
//
// Security note: this is stored plain-text in Mongo. If your deployment
// needs at-rest encryption, run the DB on an encrypted volume or slot in
// field-level encryption. The profile UI warns users about sensitivity.
type TravelerIdentity struct {
	Citizenship      string           `bson:"citizenship,omitempty"     json:"citizenship,omitempty"` // ISO-3166-1 alpha-2
	PassportNumber   string           `bson:"passport_number,omitempty" json:"passport_number,omitempty"`
	PassportExpiry   *time.Time       `bson:"passport_expiry,omitempty" json:"passport_expiry,omitempty"`
	DateOfBirth      *time.Time       `bson:"date_of_birth,omitempty"   json:"date_of_birth,omitempty"`
	KnownTravelerNo  string           `bson:"ktn,omitempty"             json:"ktn,omitempty"`
	RedressNo        string           `bson:"redress_no,omitempty"      json:"redress_no,omitempty"`
	GlobalEntryID    string           `bson:"global_entry_id,omitempty" json:"global_entry_id,omitempty"`
	LoyaltyAccounts  []LoyaltyAccount `bson:"loyalty_accounts,omitempty" json:"loyalty_accounts,omitempty"`
	EmergencyContact EmergencyContact `bson:"emergency_contact,omitempty" json:"emergency_contact,omitempty"`
}

// LoyaltyAccount: airline, hotel chain, or rental car program.
type LoyaltyAccount struct {
	Program string `bson:"program"         json:"program"` // e.g. "United MileagePlus"
	Number  string `bson:"number"          json:"number"`
	Tier    string `bson:"tier,omitempty"  json:"tier,omitempty"`
}

// EmergencyContact appears on the printable itinerary and can be shared with
// trip collaborators when the user opts in.
type EmergencyContact struct {
	Name         string `bson:"name,omitempty"         json:"name,omitempty"`
	Relationship string `bson:"relationship,omitempty" json:"relationship,omitempty"`
	Phone        string `bson:"phone,omitempty"        json:"phone,omitempty"`
	Email        string `bson:"email,omitempty"        json:"email,omitempty"`
}

// UserPreferences stores user settings
type UserPreferences struct {
	Currency      string               `bson:"currency" json:"currency"`
	Language      string               `bson:"language" json:"language"`
	Notifications NotificationSettings `bson:"notifications" json:"notifications"`
}

// NotificationSettings controls notification preferences.
//
// Sent records which "one-shot" notifications we've already delivered so the
// worker doesn't double-send on its next tick. Keys are stable identifiers
// like "<itemID>:reminder_7d" or "user:weekly_digest_2025_41". Values are
// the unix epoch at delivery time (useful for pruning later).
type NotificationSettings struct {
	PriceAlerts    bool             `bson:"price_alerts"              json:"price_alerts"`
	TripReminders  bool             `bson:"trip_reminders"            json:"trip_reminders"`
	Marketing      bool             `bson:"marketing"                 json:"marketing"`
	DiscordEnabled bool             `bson:"discord_enabled"           json:"discord_enabled"`
	DiscordWebhook string           `bson:"discord_webhook,omitempty" json:"discord_webhook,omitempty"`
	Sent           map[string]int64 `bson:"sent,omitempty"            json:"-"`
}

// Session represents an active user session
type Session struct {
	ID        primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	UserID    primitive.ObjectID `bson:"user_id" json:"user_id"`
	Token     string             `bson:"token" json:"token"`
	ExpiresAt time.Time          `bson:"expires_at" json:"expires_at"`
	IPAddress string             `bson:"ip_address,omitempty" json:"ip_address,omitempty"`
	UserAgent string             `bson:"user_agent,omitempty" json:"user_agent,omitempty"`
	CreatedAt time.Time          `bson:"created_at" json:"created_at"`
}

// NewUserPreferences returns default preferences
func NewUserPreferences() UserPreferences {
	return UserPreferences{
		Currency: "USD",
		Language: "en",
		Notifications: NotificationSettings{
			PriceAlerts:   true,
			TripReminders: true,
			Marketing:     false,
		},
	}
}

// RegisterRequest is the payload for user registration
type RegisterRequest struct {
	Email    string `json:"email" validate:"required,email"`
	Password string `json:"password" validate:"required,min=8"`
	Name     string `json:"name" validate:"required,min=2"`
}

// LoginRequest is the payload for user login
type LoginRequest struct {
	Email    string `json:"email" validate:"required,email"`
	Password string `json:"password" validate:"required"`
}

// UpdateUserRequest is the payload for updating user profile
type UpdateUserRequest struct {
	Name        string           `json:"name,omitempty"`
	AvatarURL   string           `json:"avatar_url,omitempty"`
	Preferences *UserPreferences `json:"preferences,omitempty"`
}

// ChangePasswordRequest is the payload for changing password
type ChangePasswordRequest struct {
	CurrentPassword string `json:"current_password" validate:"required"`
	NewPassword     string `json:"new_password" validate:"required,min=8"`
}

// AuthResponse is returned after successful login/register
type AuthResponse struct {
	Token string `json:"token"`
	User  *User  `json:"user"`
}
