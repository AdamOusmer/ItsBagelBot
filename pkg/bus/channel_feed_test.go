// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package bus

import (
	"testing"

	"ItsBagelBot/internal/domain/channelevents"
	"ItsBagelBot/internal/domain/overlays/feed"

	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/require"
)

func TestFeedSubjectsResolveToTheirStreams(t *testing.T) {
	for _, tc := range []struct{ subject, stream string }{
		{channelevents.Subject("42", channelevents.CommunityGift), "CHANNEL_EVENTS"},
		{feed.Subject("42", feed.FrameStats), "OVERLAY_FEED"},
	} {
		t.Run(tc.stream, func(t *testing.T) {
			stream, err := resolveStreamForTopic(tc.subject)

			require.NoError(t, err)
			require.Equal(t, tc.stream, stream)
			require.Equal(t, tc.stream, normalizedDestination(tc.subject))
		})
	}
}

func TestFeedStreamsAreBoundedMemoryStreams(t *testing.T) {
	for _, spec := range feedStreams {
		t.Run(spec.Name, func(t *testing.T) {
			require.Equal(t, nats.MemoryStorage, spec.Storage)
			require.NotZero(t, spec.MaxAge)
			require.NotZero(t, spec.MaxBytes)
		})
	}
}

func TestMessageExposesStreamSequenceAndSubject(t *testing.T) {
	wire := &nats.Msg{Subject: "channel.events.1.follow", Reply: "$JS.ACK.CHANNEL_EVENTS.durable.1.77.5.1700000000000000000.0", Data: []byte("{}")}

	msg, err := messageFromNATS(wire)

	require.NoError(t, err)
	require.Equal(t, uint64(77), msg.StreamSequence())
	require.Equal(t, "channel.events.1.follow", msg.Subject())
}
