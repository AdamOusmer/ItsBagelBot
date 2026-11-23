// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package usersrpc

import (
	"ItsBagelBot/internal/domain/consent"
	"ItsBagelBot/internal/domain/rpc"
)

const (
	MarketingSubjectPrefix   = "rpc.internal.users.marketing"
	MarketingSuppressSubject = MarketingSubjectPrefix + ".suppress"
	MarketingAudienceSubject = MarketingSubjectPrefix + ".audience"
	MarketingAudienceMaxPage = 500
)

type MarketingTokenGetRequest struct {
	Token string `json:"token"`
}

type MarketingTokenGetReply struct {
	OK          bool                    `json:"ok"`
	Status      consent.MarketingStatus `json:"status,omitempty"`
	MaskedEmail string                  `json:"masked_email,omitempty"`
	Locale      string                  `json:"locale,omitempty"`
	rpc.Refusal
}

type MarketingTokenSetRequest struct {
	Token  string                  `json:"token"`
	Status consent.MarketingStatus `json:"status"`
	Source consent.Source          `json:"source"`
}

type MarketingTokenSetReply struct {
	OK bool `json:"ok"`
	rpc.Refusal
}

type MarketingSuppressRequest struct {
	Email  string                    `json:"email"`
	Reason consent.SuppressionReason `json:"reason"`
}

type MarketingSuppressReply struct {
	OK bool `json:"ok"`
	rpc.Refusal
}

type MarketingAudienceRequest struct {
	AfterID uint64 `json:"after_id"`
	Limit   int    `json:"limit"`
}

type MarketingRecipient struct {
	UserID uint64 `json:"user_id"`
	Locale string `json:"locale"`
}

type MarketingAudienceReply struct {
	Recipients  []MarketingRecipient `json:"recipients"`
	NextAfterID uint64               `json:"next_after_id"`
	rpc.Refusal
}
