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
	Preferences  UserPreferences    `bson:"preferences" json:"preferences"`
	CreatedAt    time.Time          `bson:"created_at" json:"created_at"`
	UpdatedAt    time.Time          `bson:"updated_at" json:"updated_at"`
}

// UserPreferences stores user settings
type UserPreferences struct {
	Currency      string                `bson:"currency" json:"currency"`
	Language      string                `bson:"language" json:"language"`
	Notifications NotificationSettings  `bson:"notifications" json:"notifications"`
}

// NotificationSettings controls notification preferences
type NotificationSettings struct {
	PriceAlerts    bool `bson:"price_alerts" json:"price_alerts"`
	TripReminders  bool `bson:"trip_reminders" json:"trip_reminders"`
	Marketing      bool `bson:"marketing" json:"marketing"`
	DiscordEnabled bool `bson:"discord_enabled" json:"discord_enabled"`
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
