// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package usersrpc

import "time"

// GiveawayCandidate is the complete, canonical entry snapshot returned by
// Users. ContactEmail is deliberately represented as presence only; the
// address remains encrypted and is fetched through the existing mail RPC.
type GiveawayCandidate struct {
	UserID                    uint64     `json:"user_id"`
	Username                  string     `json:"username"`
	DisplayName               string     `json:"display_name"`
	ContactEmailAvailable     bool       `json:"contact_email_available"`
	IsActive                  bool       `json:"is_active"`
	Onboarded                 bool       `json:"onboarded"`
	Status                    string     `json:"status"`
	SubscriptionSource        string     `json:"subscription_source,omitempty"`
	SubscriptionExpiresAt     *time.Time `json:"subscription_expires_at,omitempty"`
	SubscriptionRef           *string    `json:"subscription_ref,omitempty"`
	SubscriptionCancelPending bool       `json:"subscription_cancel_pending"`
}

// PremiumGrant is the stable wire representation of one Users-owned award.
type PremiumGrant struct {
	ID                  int       `json:"id"`
	GiveawayID          string    `json:"giveaway_id"`
	AwardID             string    `json:"award_id"`
	UserID              uint64    `json:"user_id"`
	State               string    `json:"state"`
	StartAt             time.Time `json:"start_at"`
	EndAt               time.Time `json:"end_at"`
	IntervalRuleVersion string    `json:"interval_rule_version"`
}

// PremiumCoverage carries the paid agreement fields without exposing contact
// data and lists all committed giveaway intervals for the account.
type PremiumCoverage struct {
	UserID             uint64         `json:"user_id"`
	Locale             string         `json:"locale,omitempty"`
	Status             string         `json:"status"`
	IsActive           bool           `json:"is_active"`
	Banned             bool           `json:"banned"`
	TestAccount        bool           `json:"test_account"`
	PaidThrough        *time.Time     `json:"paid_through,omitempty"`
	RecurringReference *string        `json:"recurring_reference,omitempty"`
	CancelPending      bool           `json:"cancel_pending"`
	BillingUncertain   bool           `json:"billing_uncertain"`
	Grants             []PremiumGrant `json:"grants,omitempty"`
}

type PreparePremiumGrantRequest struct {
	GiveawayID          string    `json:"giveaway_id"`
	AwardID             string    `json:"award_id"`
	UserID              uint64    `json:"user_id"`
	StartAt             time.Time `json:"start_at"`
	EndAt               time.Time `json:"end_at"`
	IntervalRuleVersion string    `json:"interval_rule_version"`
}

type PreparePremiumGrantReply struct {
	Grant *PremiumGrant `json:"grant,omitempty"`
	Error string        `json:"error,omitempty"`
}

type CommitPremiumGrantRequest struct {
	GiveawayID string `json:"giveaway_id"`
	AwardID    string `json:"award_id"`
	UserID     uint64 `json:"user_id"`
}

type CommitPremiumGrantReply struct {
	Grant *PremiumGrant `json:"grant,omitempty"`
	Error string        `json:"error,omitempty"`
}

type GiveawayPoolRequest struct {
	CreatedBefore *time.Time `json:"created_before,omitempty"`
}

// GiveawayPoolReply is kept separate from repository.GiveawayPool so the
// wire package remains importable by Transactions without importing Ent.
type GiveawayPoolReply struct {
	Candidates  []GiveawayCandidate `json:"candidates,omitempty"`
	SnapshotAt  time.Time           `json:"snapshot_at"`
	RuleVersion string              `json:"rule_version"`
	Counts      GiveawayPoolCounts  `json:"counts"`
	Error       string              `json:"error,omitempty"`
}

type GiveawayPoolCounts struct {
	Total        int `json:"total"`
	Eligible     int `json:"eligible"`
	Banned       int `json:"banned"`
	Inactive     int `json:"inactive"`
	NotOnboarded int `json:"not_onboarded"`
	TestAccount  int `json:"test_account"`
	VIP          int `json:"vip"`
	CurrentStaff int `json:"current_staff"`
}

type GiveawayCoverageRequest struct {
	UserID string    `json:"user_id"`
	Now    time.Time `json:"now,omitempty"`
}

type GiveawayCoverageReply struct {
	Coverage *PremiumCoverage `json:"coverage,omitempty"`
	Error    string           `json:"error,omitempty"`
}

type GiveawayCancelRequest struct {
	GiveawayID string `json:"giveaway_id"`
	AwardID    string `json:"award_id"`
	UserID     uint64 `json:"user_id"`
}

type GiveawayCancelReply struct {
	Error string `json:"error,omitempty"`
}
