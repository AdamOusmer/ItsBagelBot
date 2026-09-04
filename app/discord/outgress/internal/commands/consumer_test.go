// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package commands

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	ddiscord "ItsBagelBot/internal/domain/discord"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/codec"

	"go.uber.org/zap"
)

var errBoom = errors.New("boom")

func testLogger() *zap.Logger { return zap.NewNop() }

// fakeCommand builds a minimal encoded Command message of the given type,
// tagged so the test can tell mod and default messages apart in the
// processed order.
func fakeCommand(t *testing.T, typ string) *bus.Message {
	t.Helper()
	raw, err := codec.Marshal(ddiscord.Command{Type: typ})
	if err != nil {
		t.Fatal(err)
	}
	return bus.NewMessage("id", raw)
}

// TestPumpDrainsModBeforeDefault is the direct test of the invariant the
// outgress command README (main.go's package doc) promises: LaneMod is
// drained to empty before LaneDefault is ever touched. Both channels are
// pre-loaded before pump starts, so the mod-only non-blocking check at the
// top of each loop iteration keeps winning for as long as mod has anything
// left -- see pump's own doc for why this is deterministic in that
// specific shape (both lanes already full) even though the general case
// has a narrow, self-correcting race.
func TestPumpDrainsModBeforeDefault(t *testing.T) {
	const modCount, defaultCount = 5, 5
	modCh := make(chan *bus.Message, modCount)
	defCh := make(chan *bus.Message, defaultCount)
	for i := 0; i < modCount; i++ {
		modCh <- fakeCommand(t, ddiscord.TypeBanMember)
	}
	for i := 0; i < defaultCount; i++ {
		defCh <- fakeCommand(t, ddiscord.TypePostChat)
	}

	var mu sync.Mutex
	var order []string
	done := make(chan struct{})

	c := &Consumer{Log: testLogger(), Handle: func(_ context.Context, cmd ddiscord.Command) error {
		mu.Lock()
		order = append(order, cmd.Type)
		n := len(order)
		mu.Unlock()
		if n == modCount+defaultCount {
			close(done)
		}
		return nil
	}}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go c.pump(ctx, modCh, defCh)

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("pump did not process every message in time")
	}

	mu.Lock()
	defer mu.Unlock()
	for i, typ := range order {
		want := ddiscord.TypeBanMember
		if i >= modCount {
			want = ddiscord.TypePostChat
		}
		if typ != want {
			t.Fatalf("order[%d] = %s, want %s (full order: %v)", i, typ, want, order)
		}
	}
}

// TestProcessAcksOnSuccessAndNacksOnFailure exercises process's own
// ack/nack decision: idempotent redelivery on a handler error, a settled
// message on success.
func TestProcessAcksOnSuccessAndNacksOnFailure(t *testing.T) {
	c := &Consumer{Log: testLogger(), Handle: func(context.Context, ddiscord.Command) error { return nil }}
	ok := fakeCommand(t, ddiscord.TypePostChat)
	c.process(ok)
	select {
	case <-ok.Acked():
	default:
		t.Fatal("successful handler must ack")
	}

	c2 := &Consumer{Log: testLogger(), Handle: func(context.Context, ddiscord.Command) error { return errBoom }}
	bad := fakeCommand(t, ddiscord.TypePostChat)
	c2.process(bad)
	select {
	case <-bad.Nacked():
	default:
		t.Fatal("failing handler must nack")
	}
}
