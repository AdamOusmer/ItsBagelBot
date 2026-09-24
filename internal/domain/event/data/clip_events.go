// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package data

const SubjectClipCreated = "data.twitch.clip.created"

type ClipCreated struct {
	BroadcasterID string `json:"broadcaster_id"`
	ClipID        string `json:"clip_id"`
	URL           string `json:"url"`
	Clipper       string `json:"clipper,omitempty"`
	Title         string `json:"title,omitempty"`
}
