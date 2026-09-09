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

// StreamInfoRequest addresses one channel, by id or by login. Exactly one is
// needed: the id is what every in-process caller already holds, and the login
// is what a chat template naming somebody else's channel ({title:pokimane})
// carries. Resolving a login to an id is Helix Get Users, which is why it
// happens on this side — sesame has no Twitch credentials at all.
type StreamInfoRequest struct {
	BroadcasterID string `json:"broadcaster_id,omitempty"`
	TargetLogin   string `json:"target_login,omitempty"`
}

// StreamInfoReply is the Get Streams title/category/viewer snapshot. It
// exists alongside UptimeReply (same underlying Helix call, StreamStartedAt
// vs StreamDetails on outgress's twitch.Client) because the two callers want
// different slices of it: !uptime only ever needs Live+StartedAt, while
// app/discord/engine's go-live fallback (see its modules/live.go's
// liveInfo) only needs Live+Title+GameName+ViewerCount.
//
// Live is false when the channel is offline, and then ViewerCount and
// StartedAt are zero — but Title and GameName are NOT: an offline channel
// still has a title and a category, and !title / !game show them, so the
// handler reads them from Get Channel Information rather than reporting the
// channel as having none. StartedAt therefore never implies live on its own;
// callers must check Live.
//
// UserFound is false only for a login Twitch does not know. It is separate
// from Error because "no such channel" is a resolved answer a caller renders
// as nothing, while an Error is a round trip that did not happen.
type StreamInfoReply struct {
	UserFound   bool      `json:"user_found"`
	Live        bool      `json:"live"`
	Title       string    `json:"title,omitempty"`
	GameName    string    `json:"game_name,omitempty"`
	ViewerCount int       `json:"viewer_count,omitempty"`
	StartedAt   time.Time `json:"started_at,omitempty"`
	Error       string    `json:"error,omitempty"`
}
