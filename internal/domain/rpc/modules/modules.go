// Package modulesrpc holds the shared wire types for the modules service RPC
// surface (the dashboard read/write verbs).
package modulesrpc

import (
	"encoding/json"

	"ItsBagelBot/internal/domain/rpc/projection"
)

// DashboardRequest covers all modules dashboard verbs; unused fields are zero.
// For the `upsert` verb Configs is the full config blob (replace); for `patch`
// it is the subset of keys to merge, and ExpectedRev enforces optimistic
// concurrency.
type DashboardRequest struct {
	UserID    string          `json:"user_id"`
	Name      string          `json:"name"`
	IsEnabled bool            `json:"is_enabled"`
	Configs   json.RawMessage `json:"configs,omitempty"`
	// ExpectedRev (patch only): the revision the client last read. When non-nil it
	// must match the stored revision or the write is rejected as a conflict.
	ExpectedRev *int `json:"expected_rev,omitempty"`
}

// DashboardReply is the reply shape for modules dashboard verbs.
type DashboardReply struct {
	Modules []projection.ModuleView `json:"modules"`
	// Rev (patch reply): the module's revision after the write, so the client can
	// carry it into its next patch.
	Rev int `json:"rev,omitempty"`
	// Conflict (patch reply): the write was rejected because ExpectedRev was stale;
	// the client should refetch and retry.
	Conflict bool   `json:"conflict,omitempty"`
	Error    string `json:"error,omitempty"`
}
