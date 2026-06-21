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
