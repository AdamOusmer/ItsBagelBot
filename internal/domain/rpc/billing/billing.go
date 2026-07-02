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
}

type ApplyReply struct {
	Applied bool   `json:"applied"`
	Error   string `json:"error,omitempty"`
}
