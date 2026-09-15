// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Package giveaways defines the private Transactions giveaway API. The
// structures in this package are deliberately independent of Ent and of the
// admin/dashboard implementations so every caller shares the same wire
// contract and no caller can smuggle provider references into a mutation.
package giveaways

import (
	"time"

	"ItsBagelBot/internal/domain/rpc"
)

// Private subjects are kept here so service wiring and dashboard callers do
// not hand-author privileged strings. The admin prefix is intentionally
// separate from the ordinary transactions RPC namespace.
const (
	AdminPrefix          = "bagel.rpc.admin.giveaways"
	MineSubject          = "bagel.rpc.transactions.giveaways.mine"
	UsersPoolSubject     = "bagel.rpc.internal.users.giveaway.pool"
	UsersCoverageSubject = "bagel.rpc.internal.users.giveaway.coverage"
	UsersPrepareSubject  = "bagel.rpc.internal.users.giveaway.prepare"
	UsersCommitSubject   = "bagel.rpc.internal.users.giveaway.commit"
	UsersCancelSubject   = "bagel.rpc.internal.users.giveaway.cancel"
	VerbCreate           = "create"
	VerbList             = "list"
	VerbPreview          = "preview"
	VerbFreeze           = "freeze"
	VerbDraw             = "draw"
	VerbGet              = "get"
	VerbRetry            = "retry"
	VerbAlerts           = "alerts"
	VerbHistory          = "history"
	VerbCapabilities     = "capabilities"
)

type Status string

const (
	StatusDraft     Status = "draft"
	StatusFrozen    Status = "frozen"
	StatusDrawn     Status = "drawn"
	StatusCancelled Status = "cancelled"
)

type AwardState string

const (
	AwardSelected    AwardState = "selected"
	AwardPreparing   AwardState = "preparing"
	AwardNeedsReview AwardState = "needs_review"
	AwardScheduled   AwardState = "scheduled"
	AwardActive      AwardState = "active"
	AwardCompleted   AwardState = "completed"
	AwardVoided      AwardState = "voided"
)

type BillingState string

const (
	BillingNotRequired BillingState = "not_required"
	BillingPending     BillingState = "pending"
	BillingProtected   BillingState = "protected"
	BillingUncertain   BillingState = "uncertain"
	BillingIncident    BillingState = "incident"
	BillingReconciled  BillingState = "reconciled"
)

type EmailState string

const (
	EmailQueued         EmailState = "queued"
	EmailAccepted       EmailState = "accepted"
	EmailFailed         EmailState = "failed"
	EmailUncertain      EmailState = "uncertain"
	EmailMissingContact EmailState = "missing_contact"
)

// Mutation identifies the authenticated operator and protects retries and
// stale dashboard tabs. ActorRole is informational; Transactions resolves the
// actor's role server-side and never trusts this field for authorization.
type Mutation struct {
	ActorID         string `json:"actor_id"`
	ActorRole       string `json:"actor_role,omitempty"`
	IdempotencyKey  string `json:"idempotency_key"`
	ExpectedVersion uint64 `json:"expected_version,omitempty"`
	Reason          string `json:"reason,omitempty"`
}

type Campaign struct {
	ID           string     `json:"id"`
	Title        string     `json:"title"`
	Reason       string     `json:"reason,omitempty"`
	RulesVersion string     `json:"rules_version"`
	WinnerCount  int        `json:"winner_count"`
	PrizeMonths  int        `json:"prize_months"`
	Status       Status     `json:"status"`
	CreatedBy    uint64     `json:"created_by"`
	Version      uint64     `json:"version"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
	FrozenAt     *time.Time `json:"frozen_at,omitempty"`
	DrawnAt      *time.Time `json:"drawn_at,omitempty"`
}

type Candidate struct {
	ID              string `json:"id,omitempty"`
	UserID          uint64 `json:"user_id"`
	Username        string `json:"username,omitempty"`
	Eligible        bool   `json:"eligible"`
	ExclusionReason string `json:"exclusion_reason,omitempty"`
	Eligibility     any    `json:"eligibility,omitempty"`
}

type PoolSummary struct {
	Total            int                 `json:"total"`
	Eligible         int                 `json:"eligible"`
	Excluded         int                 `json:"excluded"`
	Free             int                 `json:"free"`
	Premium          int                 `json:"premium"`
	Subscribers      int                 `json:"subscribers"`
	RequestedWinners int                 `json:"requested_winners"`
	PrizeMonths      int                 `json:"prize_months"`
	TotalPrizeMonths string              `json:"total_prize_months"`
	Exclusions       PoolExclusionCounts `json:"exclusions"`
}

type PoolExclusionCounts struct {
	Banned       int `json:"banned"`
	Inactive     int `json:"inactive"`
	NotOnboarded int `json:"not_onboarded"`
	TestAccount  int `json:"test_account"`
	VIP          int `json:"vip"`
	CurrentStaff int `json:"current_staff"`
}

type CreateRequest struct {
	Mutation
	Title       string `json:"title"`
	Reason      string `json:"reason,omitempty"`
	WinnerCount int    `json:"winner_count"`
	PrizeMonths int    `json:"prize_months"`
}

type CreateReply struct {
	Campaign     *Campaign    `json:"campaign,omitempty"`
	Capabilities Capabilities `json:"capabilities"`
	rpc.Refusal
}

type PreviewRequest struct {
	Mutation
	CampaignID  string `json:"campaign_id,omitempty"`
	WinnerCount int    `json:"winner_count,omitempty"`
	PrizeMonths int    `json:"prize_months,omitempty"`
}
type PreviewReply struct {
	Campaign     *Campaign    `json:"campaign,omitempty"`
	Candidates   []Candidate  `json:"candidates,omitempty"`
	Summary      PoolSummary  `json:"summary"`
	PoolDigest   string       `json:"pool_digest,omitempty"`
	Capabilities Capabilities `json:"capabilities"`
	rpc.Refusal
}

type FreezeRequest struct {
	Mutation
	CampaignID string `json:"campaign_id"`
	PoolDigest string `json:"pool_digest"`
}
type FreezeReply struct {
	Campaign     *Campaign    `json:"campaign,omitempty"`
	Summary      PoolSummary  `json:"summary"`
	Capabilities Capabilities `json:"capabilities"`
	rpc.Refusal
}

type DrawRequest struct {
	Mutation
	CampaignID string `json:"campaign_id"`
	PoolDigest string `json:"pool_digest,omitempty"`
}
type DrawReply struct {
	Draw         *Draw        `json:"draw,omitempty"`
	Awards       []Award      `json:"awards,omitempty"`
	Capabilities Capabilities `json:"capabilities"`
	rpc.Refusal
}

type Draw struct {
	ID               string    `json:"id"`
	CampaignID       string    `json:"campaign_id"`
	OperationKey     string    `json:"operation_key"`
	PoolDigest       string    `json:"pool_digest"`
	AlgorithmVersion string    `json:"algorithm_version"`
	WinnerIDs        []uint64  `json:"winner_ids"`
	ActorID          uint64    `json:"actor_id"`
	CreatedAt        time.Time `json:"created_at"`
}

type Award struct {
	ID                 string       `json:"id"`
	CampaignID         string       `json:"campaign_id"`
	DrawID             string       `json:"draw_id"`
	UserID             uint64       `json:"user_id"`
	Ordinal            uint64       `json:"ordinal"`
	PrizeMonths        int          `json:"prize_months"`
	IntervalRule       string       `json:"interval_rule"`
	State              AwardState   `json:"state"`
	BillingState       BillingState `json:"billing_state"`
	EmailState         EmailState   `json:"email_state"`
	PlannedStart       *time.Time   `json:"planned_start,omitempty"`
	PlannedEnd         *time.Time   `json:"planned_end,omitempty"`
	ConfirmedStart     *time.Time   `json:"confirmed_start,omitempty"`
	ConfirmedEnd       *time.Time   `json:"confirmed_end,omitempty"`
	GrantID            string       `json:"grant_id,omitempty"`
	BillingOperationID string       `json:"billing_operation_id,omitempty"`
	FailureReason      string       `json:"failure_reason,omitempty"`
	RetryCount         uint64       `json:"retry_count"`
	Version            uint64       `json:"version"`
	SelectedAt         time.Time    `json:"selected_at"`
	UpdatedAt          time.Time    `json:"updated_at"`
}

type AwardLookupRequest struct {
	Mutation
	AwardID string `json:"award_id"`
}
type AwardLookupReply struct {
	Award *Award `json:"award,omitempty"`
	rpc.Refusal
}
type AwardRetryRequest struct {
	Mutation
	AwardID string `json:"award_id"`
}
type AwardRetryReply struct {
	Award        *Award       `json:"award,omitempty"`
	Capabilities Capabilities `json:"capabilities"`
	rpc.Refusal
}

type ListRequest struct {
	Mutation
	Status Status `json:"status,omitempty"`
	Limit  int    `json:"limit,omitempty"`
}
type ListReply struct {
	Campaigns    []Campaign   `json:"campaigns,omitempty"`
	Capabilities Capabilities `json:"capabilities"`
	rpc.Refusal
}

type GetRequest struct {
	Mutation
	CampaignID string `json:"campaign_id"`
}
type GetReply struct {
	Campaign     *Campaign    `json:"campaign,omitempty"`
	Awards       []Award      `json:"awards,omitempty"`
	Candidates   []Candidate  `json:"candidates,omitempty"`
	Draw         *Draw        `json:"draw,omitempty"`
	Capabilities Capabilities `json:"capabilities"`
	rpc.Refusal
}

type MineRequest struct {
	UserID string `json:"user_id"`
}

func (r MineRequest) Requested() string { return r.UserID }

type MineReply struct {
	Awards []Award `json:"awards,omitempty"`
	rpc.Refusal
}

type HistoryRequest struct {
	Mutation
	UserID string `json:"user_id"`
	Limit  int    `json:"limit,omitempty"`
}
type HistoryReply struct {
	Campaigns    []Campaign   `json:"campaigns,omitempty"`
	Awards       []Award      `json:"awards,omitempty"`
	Capabilities Capabilities `json:"capabilities"`
	rpc.Refusal
}

type ListAlertsRequest struct {
	Mutation
	UnresolvedOnly bool `json:"unresolved_only"`
	Limit          int  `json:"limit,omitempty"`
}
type Alert struct {
	ID               string     `json:"id"`
	AwardID          string     `json:"award_id"`
	OperationID      string     `json:"operation_id,omitempty"`
	Category         string     `json:"category"`
	State            string     `json:"state"`
	Message          string     `json:"message"`
	AffectedBoundary *time.Time `json:"affected_boundary,omitempty"`
	FirstSeenAt      time.Time  `json:"first_seen_at"`
	LastSeenAt       time.Time  `json:"last_seen_at"`
	AcknowledgedBy   uint64     `json:"acknowledged_by,omitempty"`
	AcknowledgedAt   *time.Time `json:"acknowledged_at,omitempty"`
	ResolvedAt       *time.Time `json:"resolved_at,omitempty"`
}
type ListAlertsReply struct {
	Alerts       []Alert      `json:"alerts,omitempty"`
	Capabilities Capabilities `json:"capabilities"`
	rpc.Refusal
}

type Capabilities struct {
	NewAwardsEnabled     bool   `json:"new_awards_enabled"`
	SchedulingEnabled    bool   `json:"scheduling_enabled"`
	ProviderMutations    bool   `json:"provider_mutations_enabled"`
	IntervalRuleVerified bool   `json:"interval_rule_verified"`
	Reason               string `json:"reason,omitempty"`
}

type CapabilitiesReply struct {
	Capabilities Capabilities `json:"capabilities"`
	rpc.Refusal
}

type CapabilitiesRequest struct{ Mutation }

// ProviderMutationGate makes the launch restriction explicit. A false gate
// permits read/verification calls but refuses pause/resume mutations.
type ProviderMutationGate struct {
	Enabled bool   `json:"enabled"`
	Reason  string `json:"reason,omitempty"`
}
