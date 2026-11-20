// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package drawrpc

import "ItsBagelBot/internal/domain/rpc"

const RafflePrefix = "rpc.draw.raffle"
const RaffleStatusSubject = RafflePrefix + ".status"

const (
	VerbRaffleStart  = "start"
	VerbRaffleDraw   = "draw"
	VerbRaffleCancel = "cancel"
)

type RaffleRequest struct {
	UserID  string `json:"user_id"`
	ActorID string `json:"actor_id"`
	RoundID string `json:"round_id,omitempty"`
	Amount  int64  `json:"amount,omitempty"`
	Seconds int    `json:"seconds,omitempty"`
	Winners int    `json:"winners,omitempty"`
	Auto    bool   `json:"auto,omitempty"`
	Split   bool   `json:"split,omitempty"`
}

func (r RaffleRequest) Requested() string { return r.UserID }

type RaffleReply struct {
	RoundID  string `json:"round_id,omitempty"`
	ClosesMS int64  `json:"closes_ms,omitempty"`
	Winners  int    `json:"winners,omitempty"`
	Auto     *bool  `json:"auto,omitempty"`
	Amount   int64  `json:"amount,omitempty"`
	Done     bool   `json:"done,omitempty"`
	Reason   string `json:"reason,omitempty"`
	rpc.Refusal
}

func (r *RaffleReply) Refuse(refusal rpc.Refusal) {
	r.Refusal = refusal
	if refusal.Code == rpc.CodeInvalid {
		r.Reason = "invalid"
	}
}

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
