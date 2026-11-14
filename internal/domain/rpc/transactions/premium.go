// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package transactionsrpc

import (
	"time"

	"ItsBagelBot/internal/domain/rpc"
)

const (
	AdminPremiumPrefix = "rpc.admin.premium"
	VerbGrantEmail     = "grant_email"
	GrantEmailSubject  = AdminPremiumPrefix + "." + VerbGrantEmail
)

type GrantEmailOutcome string

const (
	GrantEmailSent    GrantEmailOutcome = "sent"
	GrantEmailNoEmail GrantEmailOutcome = "no_email"
)

type GrantEmailRequest struct {
	ActorID   string    `json:"actor_id"`
	UserID    string    `json:"user_id"`
	ExpiresAt time.Time `json:"expires_at"`
}

type GrantEmailReply struct {
	Outcome GrantEmailOutcome `json:"outcome,omitempty"`
	rpc.Refusal
}
