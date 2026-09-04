// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Package outgress defines request/reply contracts for authenticated Twitch
// reads exposed by the outgress service to Sesame.
package outgress

import "time"

type FollowageRequest struct {
	BroadcasterID string `json:"broadcaster_id"`
	TargetID      string `json:"target_id,omitempty"`
	TargetLogin   string `json:"target_login,omitempty"`
}

type FollowageReply struct {
	TargetID   string    `json:"target_id,omitempty"`
	UserFound  bool      `json:"user_found"`
	Following  bool      `json:"following"`
	FollowedAt time.Time `json:"followed_at,omitempty"`
	Error      string    `json:"error,omitempty"`
}

type AccountAgeRequest struct {
	TargetID    string `json:"target_id,omitempty"`
	TargetLogin string `json:"target_login,omitempty"`
}

type AccountAgeReply struct {
	TargetID  string    `json:"target_id,omitempty"`
	UserFound bool      `json:"user_found"`
	CreatedAt time.Time `json:"created_at,omitempty"`
	Error     string    `json:"error,omitempty"`
}

type UptimeRequest struct {
	BroadcasterID string `json:"broadcaster_id"`
}

// UptimeReply carries the current stream session's start. Live is false (and
// StartedAt zero) when the channel is offline; StartedAt alone never implies
// live, callers must check Live.
type UptimeReply struct {
	Live      bool      `json:"live"`
	StartedAt time.Time `json:"started_at,omitempty"`
	Error     string    `json:"error,omitempty"`
}

type StreamInfoRequest struct {
	BroadcasterID string `json:"broadcaster_id"`
}

// StreamInfoReply is the Get Streams title/category/viewer snapshot. It
// exists alongside UptimeReply (same underlying Helix call, StreamStartedAt
// vs StreamDetails on outgress's twitch.Client) because the two callers want
// different slices of it: !uptime only ever needs Live+StartedAt, while
// dingress's go-live fallback (see app/dingress/internal/egress/live.go's
// liveInfo) only needs Live+Title+GameName+ViewerCount. Live is false (and
// every other field zero) when the channel is offline.
type StreamInfoReply struct {
	Live        bool   `json:"live"`
	Title       string `json:"title,omitempty"`
	GameName    string `json:"game_name,omitempty"`
	ViewerCount int    `json:"viewer_count,omitempty"`
	Error       string `json:"error,omitempty"`
}
