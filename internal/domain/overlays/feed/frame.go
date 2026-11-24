// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package feed

type Frame string

const (
	FrameStats    Frame = "stats"
	FrameSubathon Frame = "subathon"
	FrameControl  Frame = "control"
	// FrameCredits carries the credit entries one event touched, as their new totals, for the page to merge by user id.
	FrameCredits Frame = "credits"
)
