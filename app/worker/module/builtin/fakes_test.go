package builtin

import (
	"context"
)

// fakeLive is a configurable module.LiveStore.
type fakeLive struct {
	live     bool
	err      error
	setCalls []uint64
	clearN   int
}

func (f *fakeLive) IsLive(_ context.Context, _ uint64) (bool, error) { return f.live, f.err }
func (f *fakeLive) SetLive(_ context.Context, id uint64) error {
	f.setCalls = append(f.setCalls, id)
	return nil
}
func (f *fakeLive) ClearLive(_ context.Context, _ uint64) error { f.clearN++; return nil }

// fakeGreet is a configurable module.GreetStore.
type fakeGreet struct {
	first   bool
	greeted []string
	resetN  int
}

func (f *fakeGreet) FirstGreet(_ context.Context, _ uint64, chatterID string) (bool, error) {
	f.greeted = append(f.greeted, chatterID)
	return f.first, nil
}
func (f *fakeGreet) ResetGreets(_ context.Context, _ uint64) error { f.resetN++; return nil }
