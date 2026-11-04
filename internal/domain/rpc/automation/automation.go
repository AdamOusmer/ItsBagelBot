// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package automationrpc

import (
	"time"

	"ItsBagelBot/internal/domain/automation"
	"ItsBagelBot/internal/domain/rpc"
	"ItsBagelBot/pkg/codec"
)

const (
	AutomationPrefix          = "bagel.rpc.modules.automation"
	ProjectionAutomationsSubj = "bagel.rpc.internal.projection.modules.automations.get"
	RunsSubject               = "bagel.rpc.automations.runs"
)

const (
	VerbList       = "list"
	VerbSummaries  = "summaries"
	VerbGet        = "get"
	VerbUpsert     = "upsert"
	VerbUpgrade    = "upgrade_alert"
	VerbDowngrade  = "downgrade_alert"
	VerbSetEnabled = "set_enabled"
	VerbDelete     = "delete"
	VerbValidate   = "validate"
)

const (
	OutcomeOK      = "ok"
	OutcomeSkipped = "skipped"
	OutcomePartial = "partial"
	OutcomeFailed  = "failed"
	OutcomeLimited = "limited"
)

const (
	StatusOK          = "ok"
	StatusSkipped     = "skipped"
	StatusUnsupported = "unsupported"
	StatusFailed      = "failed"
	StatusLimited     = "limited"
)

type ListRequest struct {
	UserID string `json:"user_id"`
}

type ListReply struct {
	Automations []automation.Automation `json:"automations"`
	rpc.Refusal
}

type SummariesReply struct {
	Automations []automation.Summary `json:"automations"`
	rpc.Refusal
}

type SetEnabledRequest struct {
	UserID           string `json:"user_id"`
	ID               string `json:"id"`
	Enabled          bool   `json:"enabled"`
	ExpectedRevision *int64 `json:"expected_revision,omitempty"`
}

type GetRequest struct {
	UserID string `json:"user_id"`
	ID     string `json:"id"`
}

type GetReply struct {
	Automation *automation.Automation `json:"automation,omitempty"`
	rpc.Refusal
}

type UpsertRequest struct {
	UserID           string           `json:"user_id"`
	ID               string           `json:"id,omitempty"`
	Name             string           `json:"name"`
	Enabled          bool             `json:"enabled"`
	Definition       codec.RawMessage `json:"definition"`
	Preview          string           `json:"preview,omitempty"`
	ExpectedRevision *int64           `json:"expected_revision,omitempty"`
}

type UpgradeAlertRequest struct {
	UserID            string           `json:"user_id"`
	Name              string           `json:"name"`
	Enabled           bool             `json:"enabled"`
	Definition        codec.RawMessage `json:"definition"`
	Preview           string           `json:"preview,omitempty"`
	EnableKey         string           `json:"enable_key"`
	ExpectedModuleRev *int             `json:"expected_module_rev,omitempty"`
}

type DowngradeAlertRequest struct {
	UserID            string `json:"user_id"`
	ID                string `json:"id"`
	EnableKey         string `json:"enable_key"`
	Message           string `json:"message"`
	Enabled           bool   `json:"enabled"`
	ExpectedRevision  *int64 `json:"expected_revision,omitempty"`
	ExpectedModuleRev *int   `json:"expected_module_rev,omitempty"`
}

type DowngradeAlertReply struct {
	Conflict bool `json:"conflict,omitempty"`
	rpc.Refusal
}

type UpsertReply struct {
	Automation *automation.Automation `json:"automation,omitempty"`
	Issues     []automation.Issue     `json:"issues,omitempty"`
	Conflict   bool                   `json:"conflict,omitempty"`
	rpc.Refusal
}

type DeleteRequest struct {
	UserID string `json:"user_id"`
	ID     string `json:"id"`
}

type DeleteReply struct {
	Deleted bool `json:"deleted"`
	rpc.Refusal
}

type ValidateRequest struct {
	UserID     string           `json:"user_id"`
	Name       string           `json:"name"`
	Definition codec.RawMessage `json:"definition"`
}

type ValidateReply struct {
	Issues []automation.Issue `json:"issues"`
	rpc.Refusal
}

type ProjectionRequest struct {
	UserID string `json:"user_id"`
}

type AutomationsProjectionReply struct {
	Automations []automation.Automation `json:"automations"`
	rpc.Refusal
}

type RunsRequest struct {
	BroadcasterID string `json:"broadcaster_id"`
	AutomationID  string `json:"automation_id,omitempty"`
	Limit         int    `json:"limit,omitempty"`
}

type RunsReply struct {
	Runs []RunRecord `json:"runs"`
	rpc.Refusal
}

type RunRecord struct {
	RunID        string         `json:"run_id"`
	AutomationID string         `json:"automation_id"`
	Name         string         `json:"name"`
	Trigger      string         `json:"trigger"`
	Source       string         `json:"source"`
	At           time.Time      `json:"at"`
	DurationMS   int            `json:"duration_ms"`
	Outcome      string         `json:"outcome"`
	Reason       string         `json:"reason,omitempty"`
	Actions      []ActionRecord `json:"actions"`
}

type ActionRecord struct {
	Index  int    `json:"index"`
	Type   string `json:"type"`
	Status string `json:"status"`
	Detail string `json:"detail,omitempty"`
}
