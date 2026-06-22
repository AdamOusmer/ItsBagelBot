// Package modulesrpc holds the shared wire types for the modules service RPC
// surface (the dashboard read/write verbs).
package modulesrpc

import (
	"encoding/json"

	"ItsBagelBot/internal/domain/rpc/projection"
)

// DashboardRequest covers all modules dashboard verbs; unused fields are zero.
type DashboardRequest struct {
	UserID    string          `json:"user_id"`
	Name      string          `json:"name"`
	IsEnabled bool            `json:"is_enabled"`
	Configs   json.RawMessage `json:"configs,omitempty"`
}

// DashboardReply is the reply shape for modules dashboard verbs.
type DashboardReply struct {
	Modules []projection.ModuleView `json:"modules"`
	Error   string                  `json:"error,omitempty"`
}
