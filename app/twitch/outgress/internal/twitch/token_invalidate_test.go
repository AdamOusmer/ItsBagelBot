// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package twitch

import (
	"context"
	"errors"
	"net/http"
	"sync/atomic"
	"testing"
	"time"
)

func TestConsumeSkipAdoptAppliesOnce(t *testing.T) {
	s := &Source{token: "dead-token"}
	s.Invalidate()

	if skip, bad := s.consumeSkipAdopt(); !skip || bad != "dead-token" {
		t.Fatalf("first consumeSkipAdopt() = (%v, %q), want (true, %q)", skip, bad, "dead-token")
	}
	if skip, bad := s.consumeSkipAdopt(); skip || bad != "" {
		t.Fatalf("second consumeSkipAdopt() = (%v, %q), want (false, \"\")", skip, bad)
	}
}

func TestSkipAdoptClearsSoLaterRefreshAdoptsAgain(t *testing.T) {
	s := &Source{token: "dead-token"}
	s.Invalidate()
	future := time.Now().Add(time.Hour)
	stored := StoredLoad{AccessToken: "dead-token", AccessTokenExpiresAt: &future}

	_, forbid := s.consumeSkipAdopt()
	if _, _, ok := adoptCandidate(stored, forbid); ok {
		t.Fatal("adoptCandidate adopted the token that was just invalidated")
	}

	_, forbid = s.consumeSkipAdopt()
	if _, _, ok := adoptCandidate(stored, forbid); !ok {
		t.Fatal("adoptCandidate still rejected the token after the skip flag cleared")
	}
}

func newInFlightRaceSource(entered, release chan struct{}, callCount *int32) *Source {
	return &Source{
		token:   "dead-token",
		expires: time.Now().Add(time.Minute),
		refresh: func(context.Context) (string, time.Duration, error) {
			if atomic.AddInt32(callCount, 1) == 1 {
				close(entered)
				<-release
				return "dead-token", time.Hour, nil
			}
			return "fresh-token", time.Hour, nil
		},
	}
}

type tokenOutcome struct {
	token string
	err   error
}

func awaitSignal(t *testing.T, ch <-chan struct{}, timeout time.Duration, timeoutMsg string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(timeout):
		t.Fatal(timeoutMsg)
	}
}

func awaitTokenOutcome(t *testing.T, done <-chan tokenOutcome, timeout time.Duration) tokenOutcome {
	t.Helper()
	select {
	case got := <-done:
		return got
	case <-time.After(timeout):
		t.Fatal("Token() never returned")
	}
	return tokenOutcome{}
}

func TestInvalidateDuringInFlightRefreshDoesNotResurrectDeadToken(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	var callCount int32
	s := newInFlightRaceSource(entered, release, &callCount)

	done := make(chan tokenOutcome, 1)
	go func() {
		token, err := s.Token(context.Background())
		done <- tokenOutcome{token, err}
	}()

	awaitSignal(t, entered, time.Second, "refresh never started")

	s.Invalidate()
	close(release)

	got := awaitTokenOutcome(t, done, time.Second)

	if got.err != nil {
		t.Fatalf("Token() error = %v", got.err)
	}
	if got.token == "dead-token" {
		t.Fatal("Token() returned the token that was invalidated while the refresh producing it " +
			"was still in flight -- the stale result was published instead of discarded")
	}
	if got.token != "fresh-token" {
		t.Fatalf("token = %q, want %q (the retried, post-invalidation refresh)", got.token, "fresh-token")
	}
	if got := atomic.LoadInt32(&callCount); got != 2 {
		t.Fatalf("refresh call count = %d, want exactly 2 (the stale attempt, then one bounded retry)", got)
	}
	if cached, ok := s.cached(0); !ok || cached == "dead-token" {
		t.Fatalf("Source's own cached state = (%q, %v), want the fresh token, not the resurrected one", cached, ok)
	}
}

func TestInvalidateForcesMintPastStillValidStoredToken(t *testing.T) {
	var mintCalls int32
	fakeTokenHTTP(t, func(*http.Request) (*http.Response, error) {
		atomic.AddInt32(&mintCalls, 1)
		return fakeOAuthResponse(`{"access_token":"minted-after-unauthorized","refresh_token":"rotated-refresh","expires_in":14400}`), nil
	})

	expiresAt := time.Now().Add(2 * time.Hour)
	src := NewStoredUserTokenSource(ClientCredentials{}, "seed-refresh-token", StoredTokenIO{
		Load: func(context.Context) StoredLoad {
			return StoredLoad{
				AccessToken:          "dead-access-token",
				AccessTokenExpiresAt: &expiresAt,
				RefreshToken:         "stored-refresh-token",
			}
		},
		Persist: func(context.Context, string, string, time.Time) error { return nil },
	}, MintLease{})

	token, err := src.Token(context.Background())
	if err != nil {
		t.Fatalf("Token() error = %v", err)
	}
	if token != "dead-access-token" {
		t.Fatalf("first Token() = %q, want the adopted stored token", token)
	}
	if got := atomic.LoadInt32(&mintCalls); got != 0 {
		t.Fatalf("mint calls before any 401 = %d, want 0", got)
	}

	src.Invalidate()

	token, err = src.Token(context.Background())
	if err != nil {
		t.Fatalf("Token() after Invalidate error = %v", err)
	}
	if token != "minted-after-unauthorized" {
		t.Fatalf("token after Invalidate = %q, want a freshly minted token, not the store's stale value", token)
	}
	if got := atomic.LoadInt32(&mintCalls); got != 1 {
		t.Fatalf("mint calls after Invalidate = %d, want exactly 1", got)
	}
}

func TestLoserAfterInvalidateDoesNotAdoptSameDeadToken(t *testing.T) {
	var mintCalls int32
	fakeTokenHTTP(t, func(*http.Request) (*http.Response, error) {
		atomic.AddInt32(&mintCalls, 1)
		return fakeOAuthResponse(`{"access_token":"minted-by-loser","expires_in":14400}`), nil
	})

	future := time.Now().Add(time.Hour)
	var loadCalls int32
	lease := MintLease{Acquire: func(context.Context) (func(), bool, bool) { return nil, false, false }}

	src := NewStoredUserTokenSource(ClientCredentials{}, "seed-refresh-token", StoredTokenIO{
		Load: func(context.Context) StoredLoad {
			atomic.AddInt32(&loadCalls, 1)
			return StoredLoad{
				AccessToken:          "dead-access-token",
				AccessTokenExpiresAt: &future,
				RefreshToken:         "stored-refresh-token",
			}
		},
		Persist: func(context.Context, string, string, time.Time) error { return nil },
	}, lease)

	token, err := src.Token(context.Background())
	if err != nil {
		t.Fatalf("Token() error = %v", err)
	}
	if token != "dead-access-token" {
		t.Fatalf("first Token() = %q, want the adopted stored token", token)
	}

	src.Invalidate()

	token, err = src.Token(context.Background())
	if err != nil {
		t.Fatalf("Token() after Invalidate error = %v", err)
	}
	if token == "dead-access-token" {
		t.Fatal("loser re-adopted the exact token that was just invalidated")
	}
	if token != "minted-by-loser" {
		t.Fatalf("token = %q, want the fallback mint", token)
	}
	if got := atomic.LoadInt32(&mintCalls); got != 1 {
		t.Fatalf("mint calls = %d, want exactly 1 (the deliberate fallback)", got)
	}
	if got, want := atomic.LoadInt32(&loadCalls), int32(2+leaseWaitAttempts); got != want {
		t.Fatalf("load calls = %d, want %d", got, want)
	}
}

func TestSkipAdoptSurvivesFailedMintAfterInvalidate(t *testing.T) {
	var mintShouldFail atomic.Bool
	mintShouldFail.Store(true)

	fakeTokenHTTP(t, func(*http.Request) (*http.Response, error) {
		if mintShouldFail.Load() {
			return nil, errors.New("id.twitch.tv unreachable")
		}
		return fakeOAuthResponse(`{"access_token":"finally-minted","refresh_token":"rotated","expires_in":14400}`), nil
	})

	expiresAt := time.Now().Add(2 * time.Hour)
	src := NewStoredUserTokenSource(ClientCredentials{}, "seed-refresh-token", StoredTokenIO{
		Load: func(context.Context) StoredLoad {
			return StoredLoad{
				AccessToken:          "dead-access-token",
				AccessTokenExpiresAt: &expiresAt,
				RefreshToken:         "stored-refresh-token",
			}
		},
		Persist: func(context.Context, string, string, time.Time) error { return nil },
	}, MintLease{})

	if _, err := src.Token(context.Background()); err != nil {
		t.Fatalf("priming Token() error = %v", err)
	}
	src.Invalidate()

	if _, err := src.Token(context.Background()); err == nil {
		t.Fatal("Token() after Invalidate succeeded, want the mint failure")
	}

	if _, err := src.Token(context.Background()); err == nil {
		t.Fatal("second Token() succeeded, want the dead token still refused")
	}

	mintShouldFail.Store(false)
	token, err := src.Token(context.Background())
	if err != nil {
		t.Fatalf("Token() after mint recovered error = %v", err)
	}
	if token != "finally-minted" {
		t.Fatalf("Token() = %q, want the freshly minted token", token)
	}
}
