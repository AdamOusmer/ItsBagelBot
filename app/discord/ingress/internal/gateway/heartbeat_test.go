// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package gateway

import (
	"context"
	"testing"
	"time"
)

func TestFirstHeartbeatLeavesBeforeTheFirstTick(t *testing.T) {
	conn := &scriptedConn{}
	sk := newSocket(conn)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		Session{}.beatLoop(ctx, sk, time.Hour, &resumeState{})
	}()
	deadline := time.Now().Add(2 * time.Second)
	for !firstOpIs(opsWritten(t, conn.wroteSnapshot()), opHeartbeat) {
		if time.Now().After(deadline) {
			cancel()
			<-done
			t.Fatal("no heartbeat left the socket inside 2s: the first beat waited for the ticker")
		}
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	<-done
}
