package models

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// PasswordReset represents a single password-reset attempt. Only the hash of
// the token is persisted; the clear-text token lives in the email link.
type PasswordReset struct {
	ID        primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	UserID    primitive.ObjectID `bson:"user_id"        json:"user_id"`
	TokenHash string             `bson:"token_hash"     json:"-"`
	ExpiresAt time.Time          `bson:"expires_at"     json:"expires_at"`
	UsedAt    *time.Time         `bson:"used_at,omitempty" json:"used_at,omitempty"`
	CreatedAt time.Time          `bson:"created_at"     json:"created_at"`
}
