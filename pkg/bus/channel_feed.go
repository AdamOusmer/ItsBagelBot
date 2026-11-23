// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package bus

import (
	"strings"
	"time"

	"ItsBagelBot/internal/domain/channelevents"
	"ItsBagelBot/internal/domain/overlays/feed"

	"github.com/nats-io/nats.go"
)

const feedMaxAge = 15 * time.Minute

// MaxBytes is the same safety valve the other memory streams carry; MaxAge is the real bound.
var ChannelEventsStream = StreamSpec{
	Name:       "CHANNEL_EVENTS",
	Subjects:   []string{channelevents.EventsMatch},
	Discard:    nats.DiscardOld,
	MaxAge:     feedMaxAge,
	Duplicates: 5 * time.Minute,
	Storage:    nats.MemoryStorage,
	Replicas:   3,
	MaxBytes:   256 << 20,
}

var OverlayFeedStream = StreamSpec{
	Name:       "OVERLAY_FEED",
	Subjects:   []string{feed.SubjectMatch},
	Discard:    nats.DiscardOld,
	MaxAge:     feedMaxAge,
	Duplicates: 5 * time.Minute,
	Storage:    nats.MemoryStorage,
	Replicas:   3,
	MaxBytes:   256 << 20,
}

var feedStreams = []StreamSpec{ChannelEventsStream, OverlayFeedStream}

func feedStreamDestination(subject string) (string, bool) {
	for _, spec := range feedStreams {
		if strings.HasPrefix(subject, strings.TrimSuffix(spec.Subjects[0], ">")) {
			return spec.Name, true
		}
	}
	return "", false
}
