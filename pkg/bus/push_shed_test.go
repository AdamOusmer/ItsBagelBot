// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package bus

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestPushLaneResumesWhileAHandlerIsStillHeld(t *testing.T) {
	forPushStreams(t, func(t *testing.T, f pushFixture) {
		t.Setenv("NATS_PULL_ACK_WAIT", "3s")
		_, inbox := f.open(t)
		f.publishIDs(t, 0, 5)
		held := receive(t, inbox)
		time.Sleep(2 * time.Second)
		var handled atomic.Int64
		go func() {
			for m := range inbox {
				handled.Add(1)
				m.Ack()
			}
		}()
		f.publishIDs(t, 5, 25)
		require.Eventually(t, func() bool { return handled.Load() >= 20 }, 8*time.Second, 25*time.Millisecond)
		held.Ack()
	})
}
