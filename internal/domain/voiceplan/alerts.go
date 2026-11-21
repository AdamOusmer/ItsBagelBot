// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package voiceplan

import "time"

const (
	DefaultMinBits      = 100
	DefaultLiveOnly     = true
	DefaultReadMessages = true
)

var styles = map[string]string{
	"warm":    "warm, friendly, upbeat",
	"hype":    "excited, energetic, celebratory",
	"calm":    "calm, gentle, relaxed",
	"playful": "playful, light, amused",
	"cozy":    "soft, cozy, intimate",
}

func Style(id string) string { return styles[id] }

type AlertKind string

type AlertDefaults struct {
	Enabled bool
	Message string
	Style   string
}

var alertDefaults = map[AlertKind]AlertDefaults{
	"follow": {false, "{user}, <short pause> thanks for the follow!", "warm"},
	"sub":    {true, "{user} just subscribed! <short pause> Welcome to the crew!", "hype"},
	"gift":   {true, "{user} gifted {count} subs! <short pause> Thank you so much!", "hype"},
	"cheer":  {true, "{user} cheered {bits} bits!", "playful"},
	"raid":   {true, "{user} is raiding with {viewers} viewers! <short pause> Welcome in!", "hype"},
}

func (kind AlertKind) Defaults() (AlertDefaults, bool) {
	defaults, ok := alertDefaults[kind]
	return defaults, ok
}

type AlertJob struct {
	EventID       string    `json:"event_id"`
	BroadcasterID string    `json:"broadcaster_id"`
	Kind          AlertKind `json:"kind"`
	Text          string    `json:"text"`
	Style         string    `json:"style"`
	Voice         string    `json:"voice"`
	CreatedAt     time.Time `json:"created_at"`
}

type OverlayEvent struct {
	ClipKey    string    `json:"clip_key"`
	EventID    string    `json:"event_id"`
	Kind       AlertKind `json:"kind"`
	DurationMS int64     `json:"duration_ms"`
	ExpiresAt  time.Time `json:"expires_at"`
}
