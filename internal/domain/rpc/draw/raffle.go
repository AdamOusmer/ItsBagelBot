// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package drawrpc

import "ItsBagelBot/internal/domain/rpc"

const RaffleStatusSubject = "rpc.draw.raffle.status"

type RaffleStatusRequest struct {
	UserID string `json:"user_id"`
	Source string `json:"source"`
}

type RaffleStatus struct {
	AccrualHealth string        `json:"accrual_health"`
	Unpaid        []string      `json:"unpaid,omitempty"`
	Rounds        []RaffleRound `json:"rounds,omitempty"`
	rpc.Refusal
}

type RaffleRound struct {
	ID       string `json:"id"`
	Phase    string `json:"phase"`
	ClosesMS int64  `json:"closes_ms"`
	Entries  int    `json:"entries"`
}
