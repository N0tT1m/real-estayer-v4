package service

import (
	"context"
	"net"
	"net/http"
	"strings"

	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/realestayer/v4/internal/logctx"
	"github.com/realestayer/v4/internal/models"
	"github.com/realestayer/v4/internal/repository"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// AuditService records sensitive operations. All writes are fire-and-forget
// from the caller's point of view (errors are logged, not returned) because
// losing an audit write should never prevent the underlying operation from
// succeeding — that would invert the user-visible contract.
//
// Callers outside an HTTP handler (e.g. background workers) can pass a nil
// *http.Request to Record; the IP/UA fields will just be empty in that row.
type AuditService struct {
	repo *repository.AuditRepository
}

func NewAuditService(repo *repository.AuditRepository) *AuditService {
	return &AuditService{repo: repo}
}

// Record writes one audit event. Returns nothing on purpose — use logs if
// you need to see why a row failed to persist.
func (s *AuditService) Record(ctx context.Context, userID primitive.ObjectID, r *http.Request, action models.AuditAction, meta map[string]any) {
	if s == nil || s.repo == nil {
		return
	}
	ev := &models.AuditEvent{
		UserID:   userID,
		Action:   action,
		Metadata: meta,
	}
	if r != nil {
		ev.IPAddress = clientIPFromRequest(r)
		ev.UserAgent = truncate(r.UserAgent(), 200)
	}
	if rid := chimw.GetReqID(ctx); rid != "" {
		ev.RequestID = rid
	}
	if err := s.repo.Create(ctx, ev); err != nil {
		logctx.From(ctx).Warn("audit: record failed",
			"action", string(action),
			"user_id", userID.Hex(),
			"error", err)
	}
}

// ListByUser returns the most recent N audit events for a user, newest first.
// Intended for the caller's own "security activity" page — authorisation is
// the HTTP handler's responsibility, not ours.
func (s *AuditService) ListByUser(ctx context.Context, userID primitive.ObjectID, limit int64) ([]models.AuditEvent, error) {
	if s == nil || s.repo == nil {
		return nil, nil
	}
	return s.repo.ListByUser(ctx, userID, limit)
}

// clientIPFromRequest prefers the first X-Forwarded-For hop, then the
// Request.RemoteAddr host. We don't try to strip private ranges — that's
// the caller's concern when they query the log.
func clientIPFromRequest(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if comma := strings.IndexByte(xff, ','); comma > 0 {
			return strings.TrimSpace(xff[:comma])
		}
		return strings.TrimSpace(xff)
	}
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max]
}
