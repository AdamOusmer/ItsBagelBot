// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"errors"
	"testing"

	"ItsBagelBot/internal/projection"

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

func TestRearmIfLiveArmsWhenLive(t *testing.T) {
	r := &countingReader{}
	s := storeWith(fakeLive{live: true}, r)

	s.RearmIfLive(context.Background(), 42)

	if r.modulesCalls != 1 {
		t.Fatalf("live broadcaster: want ArmAll to read modules once, got %d reads", r.modulesCalls)
	}
}

func TestRearmIfLiveSkipsWhenOffline(t *testing.T) {
	r := &countingReader{}
	s := storeWith(fakeLive{live: false}, r)

	s.RearmIfLive(context.Background(), 42)

	if r.modulesCalls != 0 {
		t.Fatalf("offline broadcaster: want no arm, got %d modules reads", r.modulesCalls)
	}
}

func TestRearmIfLiveSkipsOnLiveError(t *testing.T) {
	r := &countingReader{}
	s := storeWith(fakeLive{live: true, err: errors.New("valkey down")}, r)

	s.RearmIfLive(context.Background(), 42)

	if r.modulesCalls != 0 {
		t.Fatalf("live-check error: want no arm, got %d modules reads", r.modulesCalls)
	}
}

func TestRearmIfLiveSkipsZeroID(t *testing.T) {
	r := &countingReader{}
	s := storeWith(fakeLive{live: true}, r)

	s.RearmIfLive(context.Background(), 0)

	if r.modulesCalls != 0 {
		t.Fatalf("zero broadcaster id: want no arm, got %d modules reads", r.modulesCalls)
	}
}
