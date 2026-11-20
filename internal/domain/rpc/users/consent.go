// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package usersrpc

import (
	"ItsBagelBot/internal/domain/consent"
	"ItsBagelBot/internal/domain/rpc"
)

// The dashboard must derive these fields from the authenticated session.
type DashboardOwner struct {
	BroadcasterUserID string `json:"broadcaster_user_id"`
	ActorID           string `json:"actor_id"`
	DelegateOf        string `json:"delegate_of,omitempty"`
	ImpersonatorID    string `json:"impersonator_id,omitempty"`
}

func (o DashboardOwner) OwnerContext() DashboardOwner { return o }

type ConsentSetRequest struct {
	DashboardOwner
	TermsVersion string         `json:"terms_version"`
	Marketing    bool           `json:"marketing"`
	Source       consent.Source `json:"source"`
	Locale       string         `json:"locale"`
	RequestID    string         `json:"request_id"`
}

type MarketingSetRequest struct {
	DashboardOwner
	Status consent.MarketingStatus `json:"status"`
	Source consent.Source          `json:"source"`
	Locale string                  `json:"locale,omitempty"`
}

type ConsentWriteReply struct {
	OK bool `json:"ok"`
	rpc.Refusal
}
