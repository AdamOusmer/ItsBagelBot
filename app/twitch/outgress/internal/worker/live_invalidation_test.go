// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package worker

import (
	"testing"
	"time"

	"ItsBagelBot/internal/domain/invalidate"
	"ItsBagelBot/internal/valkeytest"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/cache"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

func TestLiveInvalidationSkipsMalformedIDs(t *testing.T) {
	for _, id := range []string{"42", "invalid", "18446744073709551616"} {
		t.Run(id, func(t *testing.T) {
			server := valkeytest.New(t)
			server.Handle("EVAL", func(*valkeytest.Server, valkeytest.Args) valkeytest.Reply { return valkeytest.Int(1) })
			hub := bus.NewInvalidations(nil, "", nil)
			core, logs := observer.New(zap.WarnLevel)
			writer := NewLiveWriter(server.Client(), hub, time.Hour, zap.New(core))
			var events []cache.Event
			hub.Watch(invalidate.ScopeLive, func(e cache.Event) { events = append(events, e) })
			require.NoError(t, writer.Write(t.Context(), id, true))
			if id == "42" {
				require.Equal(t, []cache.Event{{Scope: invalidate.ScopeLive, ID: 42}}, events)
				require.Zero(t, logs.Len())
				return
			}
			require.Empty(t, events)
			require.Equal(t, 1, logs.Len())
		})
	}
}
