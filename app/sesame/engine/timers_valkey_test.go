package engine

import (
	"context"
	"errors"
	"testing"

	"ItsBagelBot/internal/projection"

	"go.uber.org/zap"
)

// fakeLive is a configurable IsLiveChecker.
type fakeLive struct {
	live bool
	err  error
}

func (f fakeLive) IsLive(context.Context, uint64) (bool, error) { return f.live, f.err }

// countingReader is a projection.Reader that records how many times Modules was
// asked, so a test can tell whether RearmIfLive reached ArmAll (which is the
// first thing to read the modules view) without needing a Valkey backend.
type countingReader struct {
	modulesCalls int
}

func (r *countingReader) User(context.Context, uint64) (projection.User, error) {
	return projection.User{}, nil
}
func (r *countingReader) Modules(context.Context, uint64) ([]projection.ModuleView, error) {
	r.modulesCalls++
	return nil, nil // no "timers" module: config() bails, arm() (and Valkey) never run
}
func (r *countingReader) Command(context.Context, uint64, string) (projection.Command, bool, error) {
	return projection.Command{}, false, nil
}

// storeWith builds a store wired with just the deps RearmIfLive touches. client
// is deliberately nil: the tests assert the offline/error paths never reach an
// arm (which would dereference it), and the live path bails at an empty config
// before any Valkey call.
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
	// A live checker that would panic if consulted proves id==0 short-circuits
	// before the IsLive call.
	s := storeWith(fakeLive{live: true}, r)

	s.RearmIfLive(context.Background(), 0)

	if r.modulesCalls != 0 {
		t.Fatalf("zero broadcaster id: want no arm, got %d modules reads", r.modulesCalls)
	}
}
