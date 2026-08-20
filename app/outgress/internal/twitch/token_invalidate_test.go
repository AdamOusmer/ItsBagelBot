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

// TestConsumeSkipAdoptAppliesOnce pins consumeSkipAdopt's read-and-clear
// contract in isolation: Invalidate must arm it, and reading it once must
// clear it so a single transient 401 does not permanently disable adoption.
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

// TestSkipAdoptClearsSoLaterRefreshAdoptsAgain exercises the same contract
// through adoptCandidate, the way NewStoredUserTokenSource's closure
// actually uses it: the invalidated token is rejected while the flag is
// live, and the identical token value is adoptable again once it clears.
func TestSkipAdoptClearsSoLaterRefreshAdoptsAgain(t *testing.T) {
	s := &Source{token: "dead-token"}
	s.Invalidate()
	future := time.Now().Add(time.Hour)
	stored := StoredLoad{AccessToken: "dead-token", AccessTokenExpiresAt: &future}

	_, forbid := s.consumeSkipAdopt()
	if _, _, ok := adoptCandidate(stored, forbid); ok {
		t.Fatal("adoptCandidate adopted the token that was just invalidated")
	}

	_, forbid = s.consumeSkipAdopt() // flag already consumed above; this should be a no-op read
	if _, _, ok := adoptCandidate(stored, forbid); !ok {
		t.Fatal("adoptCandidate still rejected the token after the skip flag cleared")
	}
}

// newInFlightRaceSource builds the Source that
// TestInvalidateDuringInFlightRefreshDoesNotResurrectDeadToken races against
// Invalidate: its refresh closure blocks on the first call (so the test can
// invalidate mid-flight) and returns the pre-invalidation "dead-token";
// every later call returns "fresh-token", simulating the retried refresh
// that runs under the new generation.
//
// expires is inside refreshMargin so cached(refreshMargin) reports "due for
// renewal" and Token() actually calls refresh, instead of the fast cached
// path short-circuiting before any race can happen.
func newInFlightRaceSource(entered, release chan struct{}, callCount *int32) *Source {
	return &Source{
		token:   "dead-token",
		expires: time.Now().Add(time.Minute),
		refresh: func(context.Context) (string, time.Duration, error) {
			if atomic.AddInt32(callCount, 1) == 1 {
				close(entered)
				<-release
				// Simulates the in-flight call's view of the world as of
				// BEFORE Invalidate ran: it still sees (and would
				// adopt/return) the token that is about to be -- or already
				// has been -- rejected.
				return "dead-token", time.Hour, nil
			}
			return "fresh-token", time.Hour, nil
		},
	}
}

// tokenOutcome is Token()'s result pair, carried through a channel so a
// background goroutine can hand it back to the test.
type tokenOutcome struct {
	token string
	err   error
}

// awaitSignal blocks until ch closes or timeout elapses, failing the test on
// timeout. Used to keep the race tests' select/timeout boilerplate out of
// the test bodies themselves.
func awaitSignal(t *testing.T, ch <-chan struct{}, timeout time.Duration, timeoutMsg string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(timeout):
		t.Fatal(timeoutMsg)
	}
}

// awaitTokenOutcome blocks until done delivers a result or timeout elapses,
// failing the test on timeout.
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

// TestInvalidateDuringInFlightRefreshDoesNotResurrectDeadToken is a logic-
// race regression test (go test -race cannot catch this: every access goes
// through s.mu, so there is no data race, only a stale-write ordering bug).
//
// Sequence: a refresh is already in flight (blocked on the network) using
// generation 0, having already read consumeSkipAdopt as false because
// nothing needed skipping yet. While it is blocked, a concurrent 401 calls
// Invalidate: the cached token is cleared and gen becomes 1. The in-flight
// refresh then completes and would, before the storeIfGen fix, publish its
// result -- the token as it looked BEFORE the invalidation -- straight over
// what Invalidate just did, resurrecting a token Twitch has already
// rejected. This test drives exactly that interleaving and asserts the
// resurrected value is never what Token() hands back.
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

// TestInvalidateForcesMintPastStillValidStoredToken is the Problem 3
// regression test: a 401 must force the next refresh to mint for real, even
// though the users service still reports the just-rejected access token as
// unexpired (it has no way to know Twitch revoked it early).
func TestInvalidateForcesMintPastStillValidStoredToken(t *testing.T) {
	var mintCalls int32
	fakeTokenHTTP(t, func(*http.Request) (*http.Response, error) {
		atomic.AddInt32(&mintCalls, 1)
		return fakeOAuthResponse(`{"access_token":"minted-after-unauthorized","refresh_token":"rotated-refresh","expires_in":14400}`), nil
	})

	expiresAt := time.Now().Add(2 * time.Hour)
	src := NewStoredUserTokenSource(ClientCredentials{}, "seed-refresh-token", StoredTokenIO{
		Load: func(context.Context) StoredLoad {
			// The store never changes across this whole test: it always
			// reports the same access token, exactly as it would if nobody
			// else has minted anything yet.
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

	// This is what client.go's request() does on a 401.
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

// TestLoserAfterInvalidateDoesNotAdoptSameDeadToken is Problem 3's lease
// interaction case: a replica that just had a token 401 and lost the mint
// lease must not adopt that exact token value back out of the store, even
// though ordinary adoption logic would otherwise accept it (unexpired,
// present). It must keep waiting/falling through instead.
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
			// The store keeps reporting the SAME dead token throughout --
			// e.g. the lease key expired without anyone ever actually
			// rotating it. adoptCandidate must reject every one of these,
			// not just the first.
			return StoredLoad{
				AccessToken:          "dead-access-token",
				AccessTokenExpiresAt: &future,
				RefreshToken:         "stored-refresh-token",
			}
		},
		Persist: func(context.Context, string, string, time.Time) error { return nil },
	}, lease)

	// Prime the Source: first refresh has no skip flag, so it adopts the
	// (at this point still ordinary-looking) stored token normally.
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
	// 1 (priming) + 1 (initial Load of the post-Invalidate refresh) + the
	// full waitForAdoption budget, all rejected because they equal forbid.
	if got, want := atomic.LoadInt32(&loadCalls), int32(2+leaseWaitAttempts); got != want {
		t.Fatalf("load calls = %d, want %d", got, want)
	}
}

// TestSkipAdoptSurvivesFailedMintAfterInvalidate covers the window between a
// 401 and a successful replacement. consumeSkipAdopt clears the guard when a
// refresh starts, so a refresh that then FAILS must re-arm it; otherwise the
// next refresh adopts the very token the 401 rejected, one attempt later.
func TestSkipAdoptSurvivesFailedMintAfterInvalidate(t *testing.T) {
	var mintShouldFail atomic.Bool
	mintShouldFail.Store(true)

	fakeTokenHTTP(t, func(*http.Request) (*http.Response, error) {
		if mintShouldFail.Load() {
			return nil, errors.New("id.twitch.tv unreachable")
		}
		return fakeOAuthResponse(`{"access_token":"finally-minted","refresh_token":"rotated","expires_in":14400}`), nil
	})

	// The store keeps reporting the rejected token as unexpired for the whole
	// test: no other replica ever mints a replacement.
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

	// First refresh after the 401: consumes the guard, tries to mint, fails.
	if _, err := src.Token(context.Background()); err == nil {
		t.Fatal("Token() after Invalidate succeeded, want the mint failure")
	}

	// The guard must still be armed, so this must NOT hand back the dead
	// token even though the store still calls it unexpired.
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
