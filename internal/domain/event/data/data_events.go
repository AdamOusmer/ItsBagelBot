package data

import "encoding/json"

// Subjects form the public contract between the data services, their caches
// and the projector (see ADR 0003). Renaming one is a breaking change.
const (
	SubjectUserChanged    = "data.users.changed"
	SubjectUserDeleted    = "data.users.deleted"
	SubjectModuleChanged  = "data.modules.changed"
	SubjectCommandChanged = "data.commands.changed"
	SubjectCommandUsed    = "data.commands.used"

	// SubjectReprojectRequest asks every data service to republish its
	// current state as ordinary change events. The projector sends it on a
	// cold start so it can rebuild the Valkey projection without ever
	// reading another service's schema.
	SubjectReprojectRequest = "data.reproject.request"
)

// The DTOs carry the full new state (event-carried state transfer): consumers
// such as the Valkey projector and the in-process caches update themselves
// from the event alone and never read another service's schema.

type UserChangedDTO struct {
	UserID   uint64 `json:"user_id"`
	Username string `json:"username"`
	IsActive bool   `json:"is_active"`
	Status   string `json:"status"`
	Banned   bool   `json:"banned"`
}

type UserDeletedDTO struct {
	UserID uint64 `json:"user_id"`
}

type ModuleChangedDTO struct {
	UserID    uint64          `json:"user_id"`
	Name      string          `json:"name"`
	IsEnabled bool            `json:"is_enabled"`
	Configs   json.RawMessage `json:"configs,omitempty"`
}

type CommandChangedDTO struct {
	UserID           uint64   `json:"user_id"`
	Name             string   `json:"name"`
	Aliases          []string `json:"aliases,omitempty"`
	Response         string   `json:"response,omitempty"`
	IsActive         bool     `json:"is_active"`
	StreamOnlineOnly bool     `json:"stream_online_only"`
	Perm             string   `json:"perm,omitempty"`
	Cooldown         uint     `json:"cooldown,omitempty"`
	AllowedUserID    uint64   `json:"allowed_user_id,omitempty"`
	// Uses is the lifetime execution counter (see SubjectCommandUsed). Carried
	// on every change event so the projection never regresses it.
	Uses    uint64 `json:"uses,omitempty"`
	Deleted bool   `json:"deleted"`
}

// CommandUsedDTO reports successful executions of a custom command in chat.
// The worker aggregates ticks locally and publishes summed events on a flush
// window (rate-limiting the bus: a spammed command costs one event per window,
// not one per run). The commands service sums them into the row's lifetime
// counter on its own batch flush. Counters are loss-tolerant: a dropped event
// costs at most one window of ticks.
type CommandUsedDTO struct {
	UserID uint64 `json:"user_id"`
	Name   string `json:"name"`
	// Count of executions in the window; 0 or absent means 1.
	Count uint64 `json:"count,omitempty"`
}
