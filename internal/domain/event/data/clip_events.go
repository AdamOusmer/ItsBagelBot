// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package data

// SubjectClipCreated announces that Helix Create Clip succeeded. Outgress is
// the only publisher: the clip URL exists nowhere else until Helix returns it,
// so the fact has to originate on the lane worker that made the call.
//
// This is a FACT, not a Discord command, and the distinction is load-bearing.
// The obvious shortcut is to publish a discord_chat message onto
// DISCORD_OUTGRESS and be done. Two things break if you do:
//
//  1. DISCORD_OUTGRESS is WorkQueuePolicy. A work queue delivers each message
//     to exactly one consumer group, so the first subscriber claims the clip
//     and the dashboard clip feed and the public stats counters can never see
//     it. BAGEL_DATA is the durable replay tier (LimitsPolicy, on disk, R3):
//     every consumer binds its own durable and they do not compete.
//  2. Publishing a Discord command means outgress has to know Discord exists —
//     the module blob read, the embed builder, the enabled check. That is the
//     coupling this subject was introduced to remove. Outgress states what
//     happened on Twitch; whoever cares subscribes.
//
// Loss-tolerant like the other data.* subjects: a dropped clip event costs one
// missed archive post, never a corrupt row. The clip itself already exists on
// Twitch and was already replied to in chat before this is published.
const SubjectClipCreated = "data.twitch.clip.created"

// ClipCreated is the payload on SubjectClipCreated. BroadcasterID is the Twitch
// user id the clip belongs to, which is what a subscriber resolves its own
// per-channel config from; nothing here is Discord-shaped on purpose.
//
// Title is the clip's title as Twitch recorded it and may be empty (Helix does
// not require one). Clipper is the display name of whoever ran the command and
// is empty when the clip came from an automated path rather than a chatter.
type ClipCreated struct {
	BroadcasterID string `json:"broadcaster_id"`
	ClipID        string `json:"clip_id"`
	URL           string `json:"url"`
	Clipper       string `json:"clipper,omitempty"`
	Title         string `json:"title,omitempty"`
}
