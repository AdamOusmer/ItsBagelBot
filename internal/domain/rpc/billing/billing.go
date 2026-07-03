// Package billingrpc defines the private entitlement contract between the
// transactions service (verified Tebex webhooks) and the users service (tier
// owner). It is intentionally unavailable to dashboard/admin accounts.
package billingrpc

import "time"

type Action string

const (
	ActionActivate        Action = "activate"
	ActionCancelRequested Action = "cancel_requested"
	ActionCancelAborted   Action = "cancel_aborted"
	ActionRevoke          Action = "revoke"
)

type ApplyRequest struct {
	UserID             uint64     `json:"user_id"`
	EventID            string     `json:"event_id"`
	Action             Action     `json:"action"`
	OccurredAt         time.Time  `json:"occurred_at"`
	ExpiresAt          *time.Time `json:"expires_at,omitempty"`
	RecurringReference string     `json:"recurring_reference,omitempty"`
	// GifterID is the buyer when this activation is a gift to UserID; zero for a
	// self-purchase or renewal. When set (and this is a first-time activation),
	// the users service bumps the gifter's gifts_sent counter idempotently.
	GifterID uint64 `json:"gifter_id,omitempty"`
}

type ApplyReply struct {
	Applied bool   `json:"applied"`
	Error   string `json:"error,omitempty"`
}
