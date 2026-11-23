// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package feed

type Frame string

const (
	FrameEvent    Frame = "event"
	FrameStats    Frame = "stats"
	FrameSubathon Frame = "subathon"
	FrameControl  Frame = "control"

	SubjectRoot  = "overlay.feed."
	SubjectMatch = SubjectRoot + ">"
)

// Subject is the OVERLAY_FEED subject for one channel's frame kind.
func Subject(channelID string, frame Frame) string {
	return SubjectRoot + channelID + "." + string(frame)
}
