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

func TestActivityObserver(t *testing.T) {
	tests := []struct {
		name  string
		event engine.ObservedEvent
		want  string
	}{
		{
			name:  "localizes the command text",
			event: engine.ObservedEvent{BroadcasterID: 42, Handled: true, Command: "gift", Actor: "Gift", Locale: "fr"},
			want:  "!gift a répondu à @Gift",
		},
		{
			name:  "ignores unhandled events",
			event: engine.ObservedEvent{Handled: false, Command: "gift", Locale: "fr"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sink := &activityObserverSink{}
			activity.SetSink(sink)
			t.Cleanup(func() { activity.SetSink(nil) })

			(activityObserver{}).Observe(tt.event)

			require.Equal(t, tt.want, sink.row.Text)
		})
	}
}
