// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package automationsrpc

import "ItsBagelBot/internal/domain/rpc"

const (
	SubjectRun      = "bagel.rpc.automations.run"
	SourceDashboard = "dashboard"
)

type RunRequest struct {
	BroadcasterID string            `json:"broadcaster_id"`
	Name          string            `json:"name"`
	Args          map[string]string `json:"args,omitempty"`
	Source        string            `json:"source"`
	RequestID     string            `json:"request_id"`
}

type RunReply struct {
	Accepted bool   `json:"accepted"`
	RunID    string `json:"run_id,omitempty"`
	rpc.Refusal
}
