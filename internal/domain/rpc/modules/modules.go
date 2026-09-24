// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modulesrpc

import (
	"ItsBagelBot/internal/domain/rpc"

	"ItsBagelBot/internal/domain/rpc/projection"
	"ItsBagelBot/pkg/codec"
)

type DashboardRequest struct {
	UserID      string           `json:"user_id"`
	Name        string           `json:"name"`
	IsEnabled   bool             `json:"is_enabled"`
	Configs     codec.RawMessage `json:"configs,omitempty"`
	ExpectedRev *int             `json:"expected_rev,omitempty"`
}

type DashboardReply struct {
	Modules  []projection.ModuleView `json:"modules"`
	Rev      int                     `json:"rev,omitempty"`
	Conflict bool                    `json:"conflict,omitempty"`
	rpc.Refusal
}
