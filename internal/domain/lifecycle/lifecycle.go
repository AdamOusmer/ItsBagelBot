// Copyright (c) 2026 Adam Ousmer. All rights reserved.
package lifecycle

import "time"

const (
	Active              = "active"
	Dormant             = "dormant"
	Blocked             = "blocked"
	Disabled            = "disabled"
	Recovering          = "recovering"
	Degraded            = "degraded"
	RestrictionNone     = "none"
	RestrictionBanned   = "banned"
	RestrictionTimeout  = "timeout"
	GrantHealthy        = "healthy"
	GrantMissing        = "missing"
	GrantUnknown        = "unknown"
	DormancyPeriod      = 30 * 24 * time.Hour
	MaxRecoveryAttempts = 3
)

// State is a compact current record, not an event history. Pending notification
// and recovery work survive cache loss and retain their original identities.
type Retraction struct {
	IncidentID       string    `json:"incident_id"`
	DeliveryDeadline time.Time `json:"delivery_deadline"`
	ExpiresAt        time.Time `json:"expires_at"`
}

type State struct {
	ControlRepairAt         time.Time    `json:"control_repair_at,omitempty"`
	ControlRepairAttempts   int          `json:"control_repair_attempts,omitempty"`
	NormalCleanupAt         time.Time    `json:"normal_cleanup_at,omitempty"`
	NormalCleanupAttempts   int          `json:"normal_cleanup_attempts,omitempty"`
	GrantObservedAt         time.Time    `json:"grant_observed_at,omitempty"`
	GrantEventID            string       `json:"grant_event_id,omitempty"`
	DormancyAfter           time.Time    `json:"dormancy_after,omitempty"`
	DormancyClaimUntil      time.Time    `json:"dormancy_claim_until,omitempty"`
	DormancyProbeAt         time.Time    `json:"dormancy_probe_at,omitempty"`
	DormancyUnknownSince    time.Time    `json:"dormancy_unknown_since,omitempty"`
	RetractionDeadline      time.Time    `json:"retraction_deadline,omitempty"`
	RetractionExpiresAt     time.Time    `json:"retraction_expires_at,omitempty"`
	Retractions             []Retraction `json:"retractions,omitempty"`
	ControlListenersHealthy bool         `json:"control_listeners_healthy"`
	OwnedSubscriptions      []string     `json:"owned_subscriptions,omitempty"`
	ChatSubscriptionID      string       `json:"chat_subscription_id,omitempty"`
	Version                 int          `json:"version"`
	DesiredEnabled          bool         `json:"desired_enabled"`
	RuntimeState            string       `json:"runtime_state"`
	AdmissionGeneration     int64        `json:"admission_generation"`
	ChatRestriction         string       `json:"chat_restriction"`
	GrantHealth             string       `json:"grant_health"`
	IncidentID              string       `json:"incident_id,omitempty"`
	IncidentReason          string       `json:"incident_reason,omitempty"`
	IncidentAnnounced       bool         `json:"incident_announced"`
	NoticeDeadline          time.Time    `json:"notice_deadline,omitempty"`
	NoticeNextAttemptAt     time.Time    `json:"notice_next_attempt_at,omitempty"`
	RetractionIncidentID    string       `json:"retraction_incident_id,omitempty"`
	ModerationAt            time.Time    `json:"moderation_at,omitempty"`
	ModerationEventID       string       `json:"moderation_event_id,omitempty"`
	TimeoutUntil            time.Time    `json:"timeout_until,omitempty"`
	LastEngagedAt           time.Time    `json:"last_engaged_at,omitempty"`
	LastActivityID          string       `json:"last_activity_id,omitempty"`
	OperationID             string       `json:"operation_id,omitempty"`
	Attempts                int          `json:"attempts,omitempty"`
	NextAttemptAt           time.Time    `json:"next_attempt_at,omitempty"`
	LastFailure             string       `json:"last_failure,omitempty"`
}

func (s State) AllowsRuntime() bool {
	return s.Version == 1 && s.DesiredEnabled && s.ChatRestriction == RestrictionNone && s.GrantHealth == GrantHealthy && (s.RuntimeState == Active || s.RuntimeState == Dormant)
}
func (s State) AllowsBackground() bool { return s.AllowsRuntime() && s.RuntimeState == Active }
