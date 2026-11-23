// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package channelevents

import (
	"testing"
	"time"

	"ItsBagelBot/pkg/codec"

	"github.com/stretchr/testify/require"
)

func TestDecodeRoundTripsAGiftBundleAndRejectsUnknownFields(t *testing.T) {
	event := Event{
		ID: "evt-1", Kind: CommunityGift, ChannelID: "42", OccurredAt: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC),
		User: &User{ID: "7", Login: "gi", Name: "Gi"}, Amount: 2, Tier: "1000", CumulativeTotal: 9,
		Recipients: []User{{ID: "1"}, {ID: "2"}},
	}
	raw, err := codec.Marshal(event)
	require.NoError(t, err)

	decoded, err := Decode(raw)
	_, unknown := Decode([]byte(`{"id":"x","kind":"follow","channel_id":"1","occurred_at":"2026-01-01T00:00:00Z","extra":1}`))

	require.NoError(t, err)
	require.Equal(t, event, decoded)
	require.Error(t, unknown)
}

func TestSubjectsAreKeyedByChannelAndKind(t *testing.T) {
	require.Equal(t, "channel.events.42.cheer", Subject("42", Cheer))
	require.Equal(t, "channel.chat.42", ChatSubject("42"))
}

func TestValidateRequiresASessionChange(t *testing.T) {
	base := Event{ID: "s", Kind: Session, ChannelID: "1", OccurredAt: time.Now()}

	without := base.Validate()
	base.Change = &SessionChange{State: SessionStarted}

	require.Error(t, without)
	require.NoError(t, base.Validate())
}
