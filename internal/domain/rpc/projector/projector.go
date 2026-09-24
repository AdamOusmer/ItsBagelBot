// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package projectorrpc

import (
	"time"

	"ItsBagelBot/internal/domain/rpc/projection"
)

type DashboardRequest struct {
	UserID   string                   `json:"user_id"`
	Commands []projection.CommandView `json:"commands,omitempty"`
	Modules  []projection.ModuleView  `json:"modules,omitempty"`
}

type StatusRequest struct {
	BroadcasterID string `json:"broadcaster_id"`
}

type StatusReply struct {
	BroadcasterID string `json:"broadcaster_id"`
	Tier          string `json:"tier"`
	Banned        bool   `json:"banned"`
	Error         string `json:"error,omitempty"`
}

type LiveRequest struct {
	BroadcasterID string `json:"broadcaster_id"`
}

type LiveReply struct {
	BroadcasterID string `json:"broadcaster_id"`
	Live          bool   `json:"live"`
	Known         bool   `json:"known"`
	Error         string `json:"error,omitempty"`
}

type StreamInfoRequest struct {
	BroadcasterID string `json:"broadcaster_id"`
}

type StreamInfoReply struct {
	BroadcasterID string    `json:"broadcaster_id"`
	Title         string    `json:"title"`
	GameName      string    `json:"game_name"`
	ViewerCount   int       `json:"viewer_count"`
	PeakViewers   int       `json:"peak_viewers"`
	StartedAt     time.Time `json:"started_at"`
	EndedAt       time.Time `json:"ended_at"`
	Live          bool      `json:"live"`
	Known         bool      `json:"known"`
	Error         string    `json:"error,omitempty"`
}
