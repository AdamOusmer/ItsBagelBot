// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package usersrpc

import "ItsBagelBot/internal/domain/rpc"

const (
	AdminAlphaAuditAction = "set_alpha"
	AlphaCheckSubject     = "rpc.internal.users.alpha.check"
)

type AlphaCheckRequest struct {
	UserID string `json:"user_id"`
}

func (r AlphaCheckRequest) Requested() string { return r.UserID }

type AlphaCheckReply struct {
	Alpha bool `json:"alpha"`
	rpc.Refusal
}
