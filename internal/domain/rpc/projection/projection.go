// Package projection holds the shared wire types for the projection RPC surface.
// Consumers use these types when publishing or unmarshaling NATS request-reply
// messages on the users, commands, and modules projection subjects.
package projection

import "encoding/json"

// Request is the common input shape for all projection lookups.
type Request struct {
	UserID string `json:"user_id"`
}

// CommandView is the canonical wire shape for one custom command as stored in
// the Valkey projection. Field set and json tags match internal/projection.CommandView
// exactly so consumers can decode without conversion.
type CommandView struct {
	Name             string   `json:"name"`
	Aliases          []string `json:"aliases,omitempty"`
	Response         string   `json:"response"`
	IsActive         bool     `json:"is_active"`
	StreamOnlineOnly bool     `json:"stream_online_only"`
	Perm             string   `json:"perm"`
	Cooldown         uint     `json:"cooldown"`
	AllowedUserID    string   `json:"allowed_user_id,omitempty"`
	// Uses is the lifetime execution counter, maintained by the commands
	// service from the worker's data.commands.used events.
	Uses uint64 `json:"uses,omitempty"`
}

// ModuleView is the canonical wire shape for one module row as stored in the
// Valkey projection. Field set and json tags match internal/projection.ModuleView exactly.
type ModuleView struct {
	Name      string          `json:"name"`
	IsEnabled bool            `json:"is_enabled"`
	Configs   json.RawMessage `json:"configs,omitempty"`
}

// UserReply is the reply shape for the users projection subject.
type UserReply struct {
	UserID   string `json:"user_id"`
	Status   string `json:"status"`
	IsActive bool   `json:"is_active"`
	Banned   bool   `json:"banned"`
	Error    string `json:"error,omitempty"`
}

// CommandsReply is the reply shape for the commands projection subject.
type CommandsReply struct {
	UserID   string        `json:"user_id"`
	Commands []CommandView `json:"commands"`
	Error    string        `json:"error,omitempty"`
}

// ModulesReply is the reply shape for the modules projection subject.
type ModulesReply struct {
	UserID  string       `json:"user_id"`
	Modules []ModuleView `json:"modules"`
	Error   string       `json:"error,omitempty"`
}
