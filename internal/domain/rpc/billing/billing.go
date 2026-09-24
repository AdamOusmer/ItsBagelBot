// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package billingrpc

import "ItsBagelBot/internal/domain/rpc"

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
	GifterID           uint64     `json:"gifter_id,omitempty"`
}

type ApplyReply struct {
	Applied bool `json:"applied"`
	rpc.Refusal
}
