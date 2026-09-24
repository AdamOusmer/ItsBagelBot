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
		Load:    func(context.Context) StoredLoad { return StoredLoad{} },
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

	s.Invalidate()

	done1 := make(chan struct{})
	go func() {
		defer close(done1)
		_, _ = s.Token(context.Background())
	}()

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
