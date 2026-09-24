// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

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

type UptimeReply struct {
	Live      bool      `json:"live"`
	StartedAt time.Time `json:"started_at,omitempty"`
	Error     string    `json:"error,omitempty"`
}

type StreamInfoRequest struct {
	BroadcasterID string `json:"broadcaster_id,omitempty"`
	TargetLogin   string `json:"target_login,omitempty"`
}

type StreamInfoReply struct {
	UserFound   bool      `json:"user_found"`
	Live        bool      `json:"live"`
	Title       string    `json:"title,omitempty"`
	GameName    string    `json:"game_name,omitempty"`
	ViewerCount int       `json:"viewer_count,omitempty"`
	StartedAt   time.Time `json:"started_at,omitempty"`
	Error       string    `json:"error,omitempty"`
}

type ChannelCountsRequest struct {
	BroadcasterID string `json:"broadcaster_id"`
}

type ChannelCountsReply struct {
	Followers   int    `json:"followers"`
	FollowersOK bool   `json:"followers_ok"`
	Subs        int    `json:"subs"`
	SubsOK      bool   `json:"subs_ok"`
	Error       string `json:"error,omitempty"`
}
