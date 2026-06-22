// Package projectorrpc holds the shared wire types for the projector service RPC surface.
// The projector's commandsReply and modulesReply are field-identical to
// projection.CommandsReply and projection.ModulesReply (same json tags, same field
// names), so this package reuses those types rather than redefining them.
// Callers decoding projector dashboard replies should use projection.CommandsReply
// and projection.ModulesReply directly.
package projectorrpc

import "ItsBagelBot/internal/domain/rpc/projection"

// DashboardRequest is the input shape for projector dashboard verbs.
// Commands and Modules carry payloads for the replace verbs; they are empty for get.
type DashboardRequest struct {
	UserID   string                   `json:"user_id"`
	Commands []projection.CommandView `json:"commands,omitempty"`
	Modules  []projection.ModuleView  `json:"modules,omitempty"`
}

// StatusRequest is the input shape for the projector status verb.
type StatusRequest struct {
	BroadcasterID string `json:"broadcaster_id"`
}

// StatusReply is the reply shape for the projector status verb.
type StatusReply struct {
	BroadcasterID string `json:"broadcaster_id"`
	Tier          string `json:"tier"`
	Banned        bool   `json:"banned"`
	Error         string `json:"error,omitempty"`
}

// LiveRequest is the input shape for the projector live verb: the worker asks
// for a broadcaster's current live state when its own live key is cold.
type LiveRequest struct {
	BroadcasterID string `json:"broadcaster_id"`
}

// LiveReply is the reply shape for the projector live verb. Known is false when
// the projector could not confirm the state from its own projection and has
// escalated to Twitch (via outgress); the worker treats unknown as offline and
// the escalation refreshes the live key for the next read.
type LiveReply struct {
	BroadcasterID string `json:"broadcaster_id"`
	Live          bool   `json:"live"`
	Known         bool   `json:"known"`
	Error         string `json:"error,omitempty"`
}
