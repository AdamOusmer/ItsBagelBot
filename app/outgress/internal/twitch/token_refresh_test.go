// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package twitch

import (
	"context"
	"net/http"
	"sync/atomic"
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

// TestConcurrentGenerationsDoNotRaceOnCurrentRefresh is a data-race
// regression test for the currentRefresh field (see its doc on Source).
//
// singleflightRefresh keys its group by generation (see Invalidate), so a
// still-in-flight OLD-generation refresh and a freshly-started NEW-
// generation refresh can both reach mintOnce at the same time -- both read
// and, on a successful mint, write the rotated refresh token. Before
// currentRefresh existed, that value was a bare local captured by the
// refresh closure, which was safe only as long as singleflight guaranteed
// exactly one refresh in flight per Source; keying by generation broke that
// guarantee. `go test -race` only flags interleavings it actually executes,
// so this test exists specifically to force two live generations to call
// mintOnce at overlapping times, run under -race.
//
// The lease always grants (rather than a real Valkey lease's cross-replica
// exclusion) because the point here is to stress currentRefresh itself, not
// to re-prove lease coordination -- that is covered by the MintLease-focused
// tests in token_mintlease_test.go.
func TestConcurrentGenerationsDoNotRaceOnCurrentRefresh(t *testing.T) {
	entered := make(chan struct{})
	holdFirst := make(chan struct{})
	var reqN int32

	fakeTokenHTTP(t, func(*http.Request) (*http.Response, error) {
		if atomic.AddInt32(&reqN, 1) == 1 {
			close(entered)
			<-holdFirst
			return fakeOAuthResponse(`{"access_token":"gen0-token","refresh_token":"gen0-refresh","expires_in":14400}`), nil
		}
		return fakeOAuthResponse(`{"access_token":"gen1-token","refresh_token":"gen1-refresh","expires_in":14400}`), nil
	})

	lease := MintLease{Acquire: func(context.Context) (func(), bool, bool) {
		return func() {}, true, false
	}}

	s := NewStoredUserTokenSource(ClientCredentials{}, "seed-refresh", StoredTokenIO{
		Load:    func(context.Context) StoredLoad { return StoredLoad{} }, // never adoptable
		Persist: func(context.Context, string, string, time.Time) error { return nil },
	}, lease)

	done0 := make(chan struct{})
	go func() {
		defer close(done0)
		_, _ = s.Token(context.Background())
	}()

	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("generation 0's mint never started")
	}

	s.Invalidate() // bumps gen: generation 1's refresh is a SEPARATE singleflight call

	done1 := make(chan struct{})
	go func() {
		defer close(done1)
		_, _ = s.Token(context.Background())
	}()

	// Generation 1 is not blocked, so give it a moment to reach its own
	// mintOnce call before releasing generation 0 -- this maximizes the
	// window where both are inside getCurrentRefresh/setCurrentRefresh at
	// once. Not load-bearing for correctness, only for how reliably this
	// forces the overlap on a single run.
	time.Sleep(20 * time.Millisecond)
	close(holdFirst)

	for _, done := range []chan struct{}{done0, done1} {
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("a Token() call never returned")
		}
	}

	if got := atomic.LoadInt32(&reqN); got != 2 {
		t.Fatalf("mint requests = %d, want exactly 2 (one per generation)", got)
	}
}
