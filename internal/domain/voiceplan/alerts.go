// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package voiceplan

import "time"

const (
	DefaultMinBits  = 100
	DefaultLiveOnly = true
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
	Style   string
}

var alertDefaults = map[AlertKind]AlertDefaults{
	"sub":   {true, "hype"},
	"cheer": {true, "playful"},
}

func (kind AlertKind) Defaults() (AlertDefaults, bool) {
	defaults, ok := alertDefaults[kind]
	return defaults, ok
}

type AlertJob struct {
	EventID       string    `json:"event_id"`
	TraceID       string    `json:"trace_id,omitempty"`
	BroadcasterID string    `json:"broadcaster_id"`
	Kind          AlertKind `json:"kind"`
	Text          string    `json:"text"`
	Style         string    `json:"style"`
	Voice         string    `json:"voice"`
	CreatedAt     time.Time `json:"created_at"`

	ViewerID        string       `json:"viewer_id,omitempty"`
	Source          SpeechSource `json:"source,omitempty"`
	SourceMessageID string       `json:"source_message_id,omitempty"`
	NotBefore       time.Time    `json:"not_before,omitzero"`
}

type SpeechSource string

const (
	SourceViewer  SpeechSource = "viewer"
	SourceChannel SpeechSource = "channel"
)

type OverlayEvent struct {
	ClipKey    string    `json:"clip_key"`
	EventID    string    `json:"event_id"`
	TraceID    string    `json:"trace_id,omitempty"`
	Kind       AlertKind `json:"kind"`
	DurationMS int64     `json:"duration_ms"`
	ExpiresAt  time.Time `json:"expires_at"`
}
