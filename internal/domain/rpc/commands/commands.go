// Package commandsrpc holds the shared wire types for the commands service RPC surface.
package commandsrpc

import "ItsBagelBot/internal/domain/rpc/projection"

// DashboardRequest covers all commands dashboard verbs; unused fields are zero-valued.
type DashboardRequest struct {
	UserID           string   `json:"user_id"`
	Name             string   `json:"name"`
	Aliases          []string `json:"aliases"`
	Response         string   `json:"response"`
	IsActive         bool     `json:"is_active"`
	StreamOnlineOnly bool     `json:"stream_online_only"`
	Perm             string   `json:"perm"`
	Cooldown         uint     `json:"cooldown"`
	AllowedUserID    string   `json:"allowed_user_id"`
	// OriginalName, when set and different from Name, makes upsert a rename.
	OriginalName string `json:"original_name"`
}

// DashboardReply is the reply shape for commands dashboard verbs.
type DashboardReply struct {
	Commands []projection.CommandView `json:"commands"`
	Error    string                   `json:"error,omitempty"`
}
