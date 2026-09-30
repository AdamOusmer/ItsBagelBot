// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"errors"
	"testing"

	"ItsBagelBot/internal/projection"
	"ItsBagelBot/pkg/cache"

	"go.uber.org/zap"
)

type fakeLive struct {
	live bool
	err  error
}

func (f fakeLive) IsLive(context.Context, uint64) (bool, error) { return f.live, f.err }

type countingReader struct {
	modulesCalls int
}

func (r *countingReader) User(context.Context, uint64) (projection.User, error) {
	return projection.User{}, nil
}
func (r *countingReader) Modules(context.Context, uint64) (map[string]projection.ModuleView, error) {
	r.modulesCalls++
	return nil, nil
}

func (r *countingReader) Module(context.Context, uint64, string) (projection.ModuleView, bool, error) {
	r.modulesCalls++
	return projection.ModuleView{}, false, nil
}
func (r *countingReader) Command(context.Context, uint64, string) (projection.Command, bool, error) {
	return projection.Command{}, false, nil
}

func storeWith(live IsLiveChecker, proj projection.Reader) *ValkeyTimerStore {
	return &ValkeyTimerStore{live: live, proj: proj, log: zap.NewNop()}
}

func TestRearmArmsWhenLive(t *testing.T) {
	r := &countingReader{}
	s := storeWith(fakeLive{live: true}, r)

	s.Rearm(context.Background(), 42)

	if r.modulesCalls != 1 {
		t.Fatalf("live broadcaster: want ArmAll to read modules once, got %d reads", r.modulesCalls)
	}
}

func TestRearmArmsOnlyOfflineTimersWhenOffline(t *testing.T) {
	r := &countingReader{}
	s := storeWith(fakeLive{live: false}, r)

	s.Rearm(context.Background(), 42)

	if r.modulesCalls != 1 {
		t.Fatalf("offline broadcaster: want the offline arm path to read modules once, got %d reads", r.modulesCalls)
	}
}

func TestRearmSkipsOnLiveError(t *testing.T) {
	r := &countingReader{}
	s := storeWith(fakeLive{live: true, err: errors.New("valkey down")}, r)

	s.Rearm(context.Background(), 42)

	if r.modulesCalls != 0 {
		t.Fatalf("live-check error: want no arm, got %d modules reads", r.modulesCalls)
	}
}

func TestThrottledRearmFiresOncePerWindow(t *testing.T) {
	r := &countingReader{}
	s := storeWith(fakeLive{live: false}, r)
	s.rearmThrottle = cache.NewKeyed[uint64, bool](gatedCacheCapacity, chatRearmInterval, gatedCacheKeyFn)
	ctx := context.Background()

	s.throttledRearm(ctx, 42)
	s.throttledRearm(ctx, 42)
	s.throttledRearm(ctx, 42)

	if r.modulesCalls != 1 {
		t.Fatalf("want one rearm inside the throttle window, got %d modules reads", r.modulesCalls)
	}

	s.throttledRearm(ctx, 43)
	if r.modulesCalls != 2 {
		t.Fatalf("a different broadcaster has its own window, got %d modules reads", r.modulesCalls)
	}
}

func TestFlagsOf(t *testing.T) {
	cases := []struct {
		name   string
		timers []timerDef
		want   chatFlags
	}{
		{"none", nil, chatFlags{}},
		{"disabled timers ignored", []timerDef{{MinChatLines: 3, AllowOffline: true}}, chatFlags{}},
		{"gated", []timerDef{{Enabled: true, MinChatLines: 3}}, chatFlags{gated: true}},
		{"offline", []timerDef{{Enabled: true, AllowOffline: true}}, chatFlags{offline: true}},
		{"both across timers", []timerDef{{Enabled: true, MinChatLines: 1}, {Enabled: true, AllowOffline: true}}, chatFlags{gated: true, offline: true}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := flagsOf(c.timers); got != c.want {
				t.Fatalf("flagsOf = %+v, want %+v", got, c.want)
			}
		})
	}
}

func TestRearmSkipsZeroID(t *testing.T) {
	r := &countingReader{}
	s := storeWith(fakeLive{live: true}, r)

	s.Rearm(context.Background(), 0)

	if r.modulesCalls != 0 {
		t.Fatalf("zero broadcaster id: want no arm, got %d modules reads", r.modulesCalls)
	}
}
