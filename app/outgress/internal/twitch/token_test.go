package twitch

import (
	"context"
	"testing"
	"time"
)

func TestTokenDoesNotHoldStateLockDuringRefresh(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	s := &Source{refresh: func(context.Context) (string, time.Duration, error) {
		close(started)
		<-release
		return "token", time.Hour, nil
	}}

	done := make(chan error, 1)
	go func() {
		_, err := s.Token(context.Background())
		done <- err
	}()
	<-started

	// ExpiresIn takes the state lock. It must remain responsive while refresh
	// is blocked on external I/O.
	statusDone := make(chan struct{})
	go func() {
		_ = s.ExpiresIn()
		close(statusDone)
	}()
	select {
	case <-statusDone:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("ExpiresIn blocked behind token refresh network I/O")
	}

	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestConcurrentTokenRefreshIsCollapsed(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	calls := make(chan struct{}, 2)
	s := &Source{refresh: func(context.Context) (string, time.Duration, error) {
		calls <- struct{}{}
		close(started)
		<-release
		return "token", time.Hour, nil
	}}

	done := make(chan error, 2)
	for range 2 {
		go func() {
			_, err := s.Token(context.Background())
			done <- err
		}()
	}
	<-started
	close(release)
	for range 2 {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
	if got := len(calls); got != 1 {
		t.Fatalf("refresh calls = %d, want 1", got)
	}
}
