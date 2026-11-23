// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package feed

// Control is the alert control frame.
type Control struct {
	Paused bool `json:"paused"`
	Muted  bool `json:"muted"`
	Skip   int  `json:"skip"`
}

// Snapshot is the first frame a connection receives; Chat and Events backfill the current session.
type Snapshot struct {
	Stats    Stats      `json:"stats"`
	Subathon Subathon   `json:"subathon"`
	Control  Control    `json:"control"`
	Chat     []Delivery `json:"chat,omitempty"`
	Events   []Delivery `json:"events,omitempty"`
}
