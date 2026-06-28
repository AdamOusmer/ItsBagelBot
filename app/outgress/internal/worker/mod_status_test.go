package worker

import (
	"context"
	"errors"
	"testing"
	"time"

	"ItsBagelBot/internal/domain/rpc/manage"
	"go.uber.org/zap"
)

type fakeModRegistry struct {
	saved chan bool
}

func (f *fakeModRegistry) AcquireModCheckLock(context.Context, string, string, time.Duration) (bool, error) {
	return true, nil
}
func (f *fakeModRegistry) ReleaseModCheckLock(context.Context, string, string) error { return nil }
func (f *fakeModRegistry) SetMod(_ context.Context, _ string, isMod bool) error {
	f.saved <- isMod
	return nil
}

type fakeModeratorClient struct {
	started chan struct{}
	release chan struct{}
	err     error
}

func (f *fakeModeratorClient) HasUserToken() bool { return true }
func (f *fakeModeratorClient) IsModerator(context.Context, string, string) (bool, error) {
	close(f.started)
	<-f.release
	return true, f.err
}

func TestStaleModStatusReturnsWithoutWaitingForTwitch(t *testing.T) {
	registry := &fakeModRegistry{saved: make(chan bool, 1)}
	client := &fakeModeratorClient{started: make(chan struct{}), release: make(chan struct{})}
	verifier := newModVerifier(registry, client, "bot", "pod", zap.NewNop())
	t.Cleanup(verifier.Close)

	start := time.Now()
	got := verifier.Status(manage.Channel{IsMod: true}, true, "broadcaster", "")
	if !got {
		t.Fatal("last known moderator state was not preserved")
	}
	if elapsed := time.Since(start); elapsed > 50*time.Millisecond {
		t.Fatalf("Status blocked for %s", elapsed)
	}

	select {
	case <-client.started:
	case <-time.After(time.Second):
		t.Fatal("background verification did not start")
	}
	close(client.release)
	select {
	case isMod := <-registry.saved:
		if !isMod {
			t.Fatal("unexpected non-mod result")
		}
	case <-time.After(time.Second):
		t.Fatal("background verification did not persist")
	}
}

func TestModVerificationFailurePreservesStoredState(t *testing.T) {
	registry := &fakeModRegistry{saved: make(chan bool, 1)}
	client := &fakeModeratorClient{
		started: make(chan struct{}), release: make(chan struct{}), err: errors.New("missing scope"),
	}
	verifier := newModVerifier(registry, client, "bot", "pod", zap.NewNop())
	t.Cleanup(verifier.Close)

	if got := verifier.Status(manage.Channel{IsMod: true}, true, "broadcaster", ""); !got {
		t.Fatal("last known moderator state was not returned")
	}
	<-client.started
	close(client.release)
	select {
	case <-registry.saved:
		t.Fatal("verification failure overwrote stored moderator state")
	case <-time.After(100 * time.Millisecond):
	}
}
