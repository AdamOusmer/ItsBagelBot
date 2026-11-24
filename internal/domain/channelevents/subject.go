// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package channelevents

const (
	EventsRoot  = "channel.events."
	EventsMatch = EventsRoot + ">"
)

// Subject is the CHANNEL_EVENTS subject for one channel and kind.
func Subject(channelID string, kind Kind) string {
	return EventsRoot + channelID + "." + string(kind)
}
