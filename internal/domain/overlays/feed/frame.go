// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package feed

type Frame string

const (
	FrameStats          Frame = "stats"
	FrameSubathon       Frame = "subathon"
	FrameControl        Frame = "control"
	FrameCredits        Frame = "credits"
	FrameCreditsReplace Frame = "credits_replace"
)
