// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package usersrpc

import (
	"time"

	"ItsBagelBot/internal/domain/consent"
	"ItsBagelBot/internal/domain/rpc"
)

const (
	CampaignSubjectPrefix    = "rpc.admin.marketing"
	MarketingDeliverySubject = MarketingSubjectPrefix + ".delivery"
)

type CampaignRequest struct {
	ActorID  string                             `json:"actor_id"`
	ID       int                                `json:"id,omitempty"`
	Name     string                             `json:"name,omitempty"`
	Content  map[string]consent.CampaignContent `json:"content,omitempty"`
	Revision int                                `json:"revision,omitempty"`
}

type CampaignView struct {
	ID         int                                `json:"id"`
	Name       string                             `json:"name"`
	Revision   int                                `json:"revision"`
	Status     consent.CampaignStatus             `json:"status"`
	Content    map[string]consent.CampaignContent `json:"content"`
	CreatedBy  string                             `json:"created_by"`
	CreatedAt  time.Time                          `json:"created_at"`
	ApprovedBy string                             `json:"approved_by,omitempty"`
	ApprovedAt *time.Time                         `json:"approved_at,omitempty"`
	Sends      map[consent.SendStatus]int         `json:"sends"`
}

type CampaignReply struct {
	OK            bool           `json:"ok"`
	Campaign      *CampaignView  `json:"campaign,omitempty"`
	Campaigns     []CampaignView `json:"campaigns,omitempty"`
	Eligible      map[string]int `json:"eligible,omitempty"`
	EligibleTotal int            `json:"eligible_total,omitempty"`
	rpc.Refusal
}

type MarketingDeliveryRequest struct {
	ResendID string             `json:"resend_id"`
	Event    consent.SendStatus `json:"event"`
}

type MarketingDeliveryReply struct {
	OK bool `json:"ok"`
	rpc.Refusal
}
