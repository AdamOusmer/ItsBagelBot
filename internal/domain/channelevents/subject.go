// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package channelevents

const (
	EventsRoot  = "channel.events."
	EventsMatch = EventsRoot + ">"
	ChatRoot    = "channel.chat."
	ChatMatch   = ChatRoot + ">"
)

// Subject is the CHANNEL_EVENTS subject for one channel and kind.
func Subject(channelID string, kind Kind) string {
	return EventsRoot + channelID + "." + string(kind)
}

// ChatSubject is the core NATS subject for one channel's chat lines.
func ChatSubject(channelID string) string {
	return ChatRoot + channelID
}
