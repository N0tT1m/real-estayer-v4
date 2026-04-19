package models

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// AuditAction identifies the kind of sensitive operation being recorded.
// New actions should be added here rather than passed as free-form strings
// so downstream tooling (alerts, dashboards) stays in sync with the code.
type AuditAction string

const (
	AuditActionPasswordChanged  AuditAction = "password_changed"
	AuditActionPasswordReset    AuditAction = "password_reset"
	AuditActionIdentityUpdated  AuditAction = "identity_updated"
	AuditActionTripShareEnabled AuditAction = "trip_share_enabled"
	AuditActionTripShareRevoked AuditAction = "trip_share_revoked"
	AuditActionTOTPEnabled      AuditAction = "totp_enabled"
	AuditActionTOTPDisabled     AuditAction = "totp_disabled"
)

// AuditEvent is one row in the audit log.
//
// We keep the schema narrow on purpose: the log must be append-only-friendly
// and cheap to scan. Context-specific details (what changed) go in Metadata
// as a bson map; do NOT put PII (e.g. passport numbers) there — the point is
// to record *that* the change happened, not its contents.
type AuditEvent struct {
	ID        primitive.ObjectID `bson:"_id,omitempty"    json:"id"`
	UserID    primitive.ObjectID `bson:"user_id"          json:"user_id"`
	Action    AuditAction        `bson:"action"           json:"action"`
	IPAddress string             `bson:"ip,omitempty"     json:"ip,omitempty"`
	UserAgent string             `bson:"ua,omitempty"     json:"ua,omitempty"`
	RequestID string             `bson:"rid,omitempty"    json:"rid,omitempty"`
	Metadata  map[string]any     `bson:"meta,omitempty"   json:"meta,omitempty"`
	CreatedAt time.Time          `bson:"created_at"       json:"created_at"`
}
