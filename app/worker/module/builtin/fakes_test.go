package builtin

import (
	"context"
	"time"

	"ItsBagelBot/internal/projection"
)

// fakeReader is a configurable projection.Reader for the builtin tests.
type fakeReader struct {
	user    projection.User
	command projection.Command
	found   bool
	modules []projection.ModuleView
	cmdErr  error
	modErr  error
	// override controls CommandOverride. ovDisabled disables the default
	// command; ovResponse rewords it. Zero value means "no row: enabled".
	ovDisabled bool
	ovResponse string
	ovErr      error
}

func (f fakeReader) User(context.Context, uint64) (projection.User, error) { return f.user, nil }
func (f fakeReader) Modules(context.Context, uint64) ([]projection.ModuleView, error) {
	return f.modules, f.modErr
}
func (f fakeReader) Command(context.Context, uint64, string) (projection.Command, bool, error) {
	return f.command, f.found, f.cmdErr
}
func (f fakeReader) CommandOverride(context.Context, uint64, string) (bool, string, error) {
	return !f.ovDisabled, f.ovResponse, f.ovErr
}

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

// fakeCooldown is a configurable module.CooldownStore.
type fakeCooldown struct {
	allow bool
	calls int
}

func (f *fakeCooldown) Allow(context.Context, string, time.Duration) (bool, error) {
	f.calls++
	return f.allow, nil
}

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
