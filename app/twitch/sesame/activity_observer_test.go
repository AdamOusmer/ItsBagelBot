// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package main

import (
	"context"
	"testing"

	"ItsBagelBot/app/twitch/sesame/engine"
	"ItsBagelBot/internal/activity"
	"github.com/stretchr/testify/require"
)

type activityObserverSink struct{ row activity.Row }

func (s *activityObserverSink) Emit(_ context.Context, _ string, row activity.Row) { s.row = row }

func TestActivityObserverLocalizesCommandText(t *testing.T) {
	sink := &activityObserverSink{}
	activity.SetSink(sink)
	t.Cleanup(func() { activity.SetSink(nil) })

	(activityObserver{}).Observe(engine.ObservedEvent{
		BroadcasterID: 42,
		Handled:       true,
		Command:       "gift",
		Actor:         "Gift",
		Locale:        "fr",
	})

	require.Equal(t, "!gift a répondu à @Gift", sink.row.Text)
}

func TestActivityObserverIgnoresUnhandledEvents(t *testing.T) {
	sink := &activityObserverSink{}
	activity.SetSink(sink)
	t.Cleanup(func() { activity.SetSink(nil) })

	(activityObserver{}).Observe(engine.ObservedEvent{Handled: false, Command: "gift", Locale: "fr"})
	require.Empty(t, sink.row.Text)
}
