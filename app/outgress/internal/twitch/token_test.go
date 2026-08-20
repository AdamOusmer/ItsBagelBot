// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package twitch

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// fakeTokenHTTP swaps the package-level tokenHTTP client for the duration of
// the test, so postToken's request to id.twitch.tv never leaves the process.
// Restored automatically via t.Cleanup.
func fakeTokenHTTP(t *testing.T, handler roundTripFunc) {
	t.Helper()
	orig := tokenHTTP
	tokenHTTP = &http.Client{Transport: handler}
	t.Cleanup(func() { tokenHTTP = orig })
}

// fakeOAuthResponse builds the http.Response postToken expects from a
// successful grant.
func fakeOAuthResponse(body string) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     make(http.Header),
	}
}

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

// TestAdoptableStored covers adoptableStored in isolation: it's the decision
// at the heart of the mint-avoidance fix (see NewStoredUserTokenSource's
// doc), so it gets its own table rather than only being exercised indirectly
// through a full Source.
func TestAdoptableStored(t *testing.T) {
	future := time.Now().Add(time.Hour)
	nearExpiry := time.Now().Add(refreshMargin - time.Second)

	cases := []struct {
		name   string
		stored StoredLoad
		wantOK bool
	}{
		{"no access token", StoredLoad{AccessTokenExpiresAt: &future}, false},
		{"no expiry -- must never mean valid forever", StoredLoad{AccessToken: "tok"}, false},
		{"expiry inside refreshMargin", StoredLoad{AccessToken: "tok", AccessTokenExpiresAt: &nearExpiry}, false},
		{"expiry comfortably in the future", StoredLoad{AccessToken: "tok", AccessTokenExpiresAt: &future}, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			token, ttl, ok := adoptableStored(tc.stored)
			if ok != tc.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tc.wantOK)
			}
			if !ok {
				return
			}
			if token != tc.stored.AccessToken {
				t.Fatalf("token = %q, want %q", token, tc.stored.AccessToken)
			}
			if ttl <= 0 {
				t.Fatalf("ttl = %v, want > 0", ttl)
			}
		})
	}
}

// TestStoredUserTokenSourceAdoptsStoredAccessToken is the adoption path this
// whole fix exists for: a comfortably-valid stored access token must be
// served straight from Load, with no mint (no call to Persist, and -- since
// this test never provides a refresh token that could reach postToken --
// implicitly no network call to Twitch either; if the closure fell through
// to minting it would hit ErrNoRefreshToken or a real network dial, not
// return the stored token).
func TestStoredUserTokenSourceAdoptsStoredAccessToken(t *testing.T) {
	expiresAt := time.Now().Add(2 * time.Hour)
	persistCalled := false

	src := NewStoredUserTokenSource(ClientCredentials{}, "", StoredTokenIO{
		Load: func(context.Context) StoredLoad {
			return StoredLoad{
				AccessToken:          "stored-access-token",
				AccessTokenExpiresAt: &expiresAt,
				RefreshToken:         "stored-refresh-token",
			}
		},
		Persist: func(context.Context, string, string, time.Time) error {
			persistCalled = true
			return nil
		},
	}, MintLease{})

	token, err := src.Token(context.Background())
	if err != nil {
		t.Fatalf("Token() error = %v", err)
	}
	if token != "stored-access-token" {
		t.Fatalf("token = %q, want %q", token, "stored-access-token")
	}
	if persistCalled {
		t.Fatal("Persist was called; adopting a stored token must never re-mint or re-persist")
	}
	if remaining := src.ExpiresIn(); remaining <= refreshMargin {
		t.Fatalf("ExpiresIn() = %v, want > refreshMargin (%v)", remaining, refreshMargin)
	}
}

// TestStoredUserTokenSourceFallsBackWithoutStoredExpiry is the backward-
// compatibility case: a reply that carries an access token but no expiry --
// exactly what an old row (written before this field existed) or an old
// users-service build (whose reply simply omits the new field) produces --
// must be treated as unusable, never as "valid forever". With no refresh
// token available either, the closure has nothing left to fall back to and
// must report ErrNoRefreshToken rather than adopt the token or hang trying
// to reach Twitch.
func TestStoredUserTokenSourceFallsBackWithoutStoredExpiry(t *testing.T) {
	src := NewStoredUserTokenSource(ClientCredentials{}, "", StoredTokenIO{
		Load: func(context.Context) StoredLoad {
			return StoredLoad{AccessToken: "stored-access-token"} // no AccessTokenExpiresAt, no RefreshToken
		},
		Persist: func(context.Context, string, string, time.Time) error {
			t.Fatal("Persist must not be called when there is nothing to mint")
			return nil
		},
	}, MintLease{})

	_, err := src.Token(context.Background())
	if err != ErrNoRefreshToken {
		t.Fatalf("err = %v, want ErrNoRefreshToken", err)
	}
}

// TestStoredUserTokenSourceFallsBackWhenStoredTokenNearExpiry mirrors the
// previous test for the other unusable case: an access token whose remaining
// life is inside refreshMargin must not be adopted even though its expiry IS
// known.
func TestStoredUserTokenSourceFallsBackWhenStoredTokenNearExpiry(t *testing.T) {
	nearExpiry := time.Now().Add(refreshMargin - time.Second)

	src := NewStoredUserTokenSource(ClientCredentials{}, "", StoredTokenIO{
		Load: func(context.Context) StoredLoad {
			return StoredLoad{AccessToken: "stored-access-token", AccessTokenExpiresAt: &nearExpiry}
		},
		Persist: func(context.Context, string, string, time.Time) error {
			t.Fatal("Persist must not be called when there is nothing to mint")
			return nil
		},
	}, MintLease{})

	_, err := src.Token(context.Background())
	if err != ErrNoRefreshToken {
		t.Fatalf("err = %v, want ErrNoRefreshToken", err)
	}
}

// TestMintLeaseWinnerMintsExactlyOnce covers the winning side of Problem 2's
// fix: with a MintLease that always grants, exactly one postToken call
// happens, and the lease is released only after Persist lands (see
// mintLeased's doc for why the release is not immediate).
func TestMintLeaseWinnerMintsExactlyOnce(t *testing.T) {
	var mintCalls int32
	fakeTokenHTTP(t, func(*http.Request) (*http.Response, error) {
		atomic.AddInt32(&mintCalls, 1)
		return fakeOAuthResponse(`{"access_token":"winner-token","refresh_token":"rotated","expires_in":14400}`), nil
	})

	persisted := make(chan struct{})
	releaseCh := make(chan struct{})
	lease := MintLease{Acquire: func(context.Context) (func(), bool, bool) {
		return func() { close(releaseCh) }, true, false
	}}

	src := NewStoredUserTokenSource(ClientCredentials{}, "seed-refresh", StoredTokenIO{
		Load: func(context.Context) StoredLoad { return StoredLoad{} }, // nothing stored: never adoptable
		Persist: func(context.Context, string, string, time.Time) error {
			close(persisted)
			return nil
		},
	}, lease)

	token, err := src.Token(context.Background())
	if err != nil {
		t.Fatalf("Token() error = %v", err)
	}
	if token != "winner-token" {
		t.Fatalf("token = %q, want %q", token, "winner-token")
	}
	if got := atomic.LoadInt32(&mintCalls); got != 1 {
		t.Fatalf("mint calls = %d, want 1", got)
	}

	select {
	case <-persisted:
	case <-time.After(time.Second):
		t.Fatal("Persist was never called by the winner")
	}
	select {
	case <-releaseCh:
	case <-time.After(time.Second):
		t.Fatal("lease was never released after Persist completed")
	}
}

// TestMintLeaseLoserAdoptsWithoutMinting covers Problem 2's other side: a
// replica that loses the lease must poll the store and adopt the winner's
// token once it appears, never calling postToken itself.
func TestMintLeaseLoserAdoptsWithoutMinting(t *testing.T) {
	var mintCalls int32
	fakeTokenHTTP(t, func(*http.Request) (*http.Response, error) {
		atomic.AddInt32(&mintCalls, 1)
		return fakeOAuthResponse(`{"access_token":"should-not-be-minted","expires_in":14400}`), nil
	})

	future := time.Now().Add(time.Hour)
	var loadCalls int32
	lease := MintLease{Acquire: func(context.Context) (func(), bool, bool) { return nil, false, false }}

	src := NewStoredUserTokenSource(ClientCredentials{}, "seed-refresh", StoredTokenIO{
		Load: func(context.Context) StoredLoad {
			// The first two Loads (the initial one plus the first
			// waitForAdoption poll) see nothing yet; the winner "persists"
			// on the third.
			if atomic.AddInt32(&loadCalls, 1) < 3 {
				return StoredLoad{RefreshToken: "stored-refresh-token"}
			}
			return StoredLoad{
				AccessToken:          "winners-token",
				AccessTokenExpiresAt: &future,
				RefreshToken:         "stored-refresh-token",
			}
		},
		Persist: func(context.Context, string, string, time.Time) error {
			t.Fatal("a loser must never persist; it never minted")
			return nil
		},
	}, lease)

	token, err := src.Token(context.Background())
	if err != nil {
		t.Fatalf("Token() error = %v", err)
	}
	if token != "winners-token" {
		t.Fatalf("token = %q, want the winner's persisted token", token)
	}
	if got := atomic.LoadInt32(&mintCalls); got != 0 {
		t.Fatalf("mint calls = %d, want 0 -- adoption succeeded, minting must not happen", got)
	}
}

// TestMintLeaseLoserAcquiresWhenWinnerVanishes covers the Safety Bug 2 fix:
// a losing replica must also try to acquire the lease on every poll, not
// just wait for an adopted token to appear. When the original winner is
// gone -- crashed mid-mint, or simply let mintLeaseTTL expire -- the lease
// frees up mid-wait, and the loser that acquires it becomes the new winner
// and mints SAFELY through mintLeased (which releases the lease itself),
// rather than falling through to the uncoordinated last-resort mint.
//
// The lease's Acquire fakes exactly that timeline: the first call (from
// mintOrAdopt's own attempt) loses, as if another replica already held it;
// the second call (the wait loop's first poll) finds it free.
func TestMintLeaseLoserAcquiresWhenWinnerVanishes(t *testing.T) {
	var mintCalls int32
	fakeTokenHTTP(t, func(*http.Request) (*http.Response, error) {
		atomic.AddInt32(&mintCalls, 1)
		return fakeOAuthResponse(`{"access_token":"acquired-by-loser","expires_in":14400}`), nil
	})

	var acquireCalls int32
	released := make(chan struct{})
	lease := MintLease{Acquire: func(context.Context) (func(), bool, bool) {
		if atomic.AddInt32(&acquireCalls, 1) == 1 {
			return nil, false, false
		}
		return func() { close(released) }, true, false
	}}

	src := NewStoredUserTokenSource(ClientCredentials{}, "seed-refresh", StoredTokenIO{
		Load:    func(context.Context) StoredLoad { return StoredLoad{} }, // never adoptable
		Persist: func(context.Context, string, string, time.Time) error { return nil },
	}, lease)

	token, err := src.Token(context.Background())
	if err != nil {
		t.Fatalf("Token() error = %v", err)
	}
	if token != "acquired-by-loser" {
		t.Fatalf("token = %q, want %q", token, "acquired-by-loser")
	}
	if got := atomic.LoadInt32(&mintCalls); got != 1 {
		t.Fatalf("mint calls = %d, want exactly 1", got)
	}
	select {
	case <-released:
	case <-time.After(time.Second):
		t.Fatal("loser minted without going through mintLeased -- it must acquire the lease " +
			"and release it itself, not fall through uncoordinated")
	}
}

// TestMintLeaseSkipsWaitWhenBackendUnavailable covers the fix for the
// second reviewed defect: when the lease backend itself is unreachable
// (Valkey down/timed out), MintLease.Acquire reports unavailable == true,
// and mintOrAdopt must skip the whole leaseWaitAttempts*leaseWaitInterval
// adoption-poll budget and mint immediately -- waiting on a backend nobody
// can read is guaranteed wasted time. This is asserted by never satisfying
// an adoptable token and never granting the lease, then checking the mint
// happens with (close to) zero added latency instead of leaseWaitAttempts
// worth of polling.
func TestMintLeaseSkipsWaitWhenBackendUnavailable(t *testing.T) {
	var mintCalls int32
	fakeTokenHTTP(t, func(*http.Request) (*http.Response, error) {
		atomic.AddInt32(&mintCalls, 1)
		return fakeOAuthResponse(`{"access_token":"unavailable-fallback-mint","expires_in":14400}`), nil
	})

	var loadCalls int32
	lease := MintLease{Acquire: func(context.Context) (func(), bool, bool) {
		return nil, false, true // backend unavailable, every call
	}}

	src := NewStoredUserTokenSource(ClientCredentials{}, "seed-refresh", StoredTokenIO{
		Load: func(context.Context) StoredLoad {
			atomic.AddInt32(&loadCalls, 1)
			return StoredLoad{} // never adoptable; would only be reached by the wait loop
		},
		Persist: func(context.Context, string, string, time.Time) error { return nil },
	}, lease)

	start := time.Now()
	token, err := src.Token(context.Background())
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("Token() error = %v", err)
	}
	if token != "unavailable-fallback-mint" {
		t.Fatalf("token = %q, want %q", token, "unavailable-fallback-mint")
	}
	if got := atomic.LoadInt32(&mintCalls); got != 1 {
		t.Fatalf("mint calls = %d, want exactly 1", got)
	}
	// Exactly one Load: the ordinary pre-mint adoption check every refresh
	// does (see NewStoredUserTokenSource's closure), never more. The
	// leaseWaitAttempts-sized poll loop must not have run at all -- if it
	// had, this would be 1+leaseWaitAttempts, same shape as
	// TestMintLeaseLoserFallsThroughWhenNothingAppears.
	if got := atomic.LoadInt32(&loadCalls); got != 1 {
		t.Fatalf("load calls = %d, want exactly 1 -- an unavailable backend must skip the "+
			"adoption poll loop entirely, not just skip minting through the lease", got)
	}
	if elapsed >= leaseWaitInterval {
		t.Fatalf("Token() took %s, want well under one leaseWaitInterval (%s) -- "+
			"an unavailable lease backend must not pay the wait budget", elapsed, leaseWaitInterval)
	}
}

// TestMintLeaseLoserFallsThroughWhenNothingAppears covers the deliberate
// trade-off documented on mintOrAdopt's fallback: when the wait budget
// (leaseWaitAttempts) is exhausted and the store never shows an adoptable
// token (winner crashed, Valkey lost the key, ...), the loser mints anyway
// rather than leaving the grant stuck.
func TestMintLeaseLoserFallsThroughWhenNothingAppears(t *testing.T) {
	var mintCalls int32
	fakeTokenHTTP(t, func(*http.Request) (*http.Response, error) {
		atomic.AddInt32(&mintCalls, 1)
		return fakeOAuthResponse(`{"access_token":"fallback-mint","expires_in":14400}`), nil
	})

	var loadCalls int32
	lease := MintLease{Acquire: func(context.Context) (func(), bool, bool) { return nil, false, false }}

	src := NewStoredUserTokenSource(ClientCredentials{}, "seed-refresh", StoredTokenIO{
		Load: func(context.Context) StoredLoad {
			atomic.AddInt32(&loadCalls, 1)
			return StoredLoad{RefreshToken: "stored-refresh-token"} // never becomes adoptable
		},
		Persist: func(context.Context, string, string, time.Time) error { return nil },
	}, lease)

	token, err := src.Token(context.Background())
	if err != nil {
		t.Fatalf("Token() error = %v", err)
	}
	if token != "fallback-mint" {
		t.Fatalf("token = %q, want %q", token, "fallback-mint")
	}
	if got := atomic.LoadInt32(&mintCalls); got != 1 {
		t.Fatalf("mint calls = %d, want exactly 1 (the deliberate fallback)", got)
	}
	if got, want := atomic.LoadInt32(&loadCalls), int32(1+leaseWaitAttempts); got != want {
		t.Fatalf("load calls = %d, want %d (1 initial + the full wait budget)", got, want)
	}
}

// TestMintLeaseAbsentMintsUncoordinated covers the zero-value MintLease{}:
// with Acquire == nil, mintOrAdopt must behave exactly as it did before this
// fix existed -- mint straight away, no Valkey round trip, no wait.
func TestMintLeaseAbsentMintsUncoordinated(t *testing.T) {
	var mintCalls int32
	fakeTokenHTTP(t, func(*http.Request) (*http.Response, error) {
		atomic.AddInt32(&mintCalls, 1)
		return fakeOAuthResponse(`{"access_token":"uncoordinated-mint","expires_in":14400}`), nil
	})

	src := NewStoredUserTokenSource(ClientCredentials{}, "seed-refresh", StoredTokenIO{
		Load:    func(context.Context) StoredLoad { return StoredLoad{} },
		Persist: func(context.Context, string, string, time.Time) error { return nil },
	}, MintLease{})

	token, err := src.Token(context.Background())
	if err != nil {
		t.Fatalf("Token() error = %v", err)
	}
	if token != "uncoordinated-mint" {
		t.Fatalf("token = %q, want %q", token, "uncoordinated-mint")
	}
	if got := atomic.LoadInt32(&mintCalls); got != 1 {
		t.Fatalf("mint calls = %d, want 1", got)
	}
}

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

	// expires is inside refreshMargin so cached(refreshMargin) reports "due
	// for renewal" and Token() actually calls refresh, instead of the fast
	// cached path short-circuiting before any race can happen.
	s := &Source{token: "dead-token", expires: time.Now().Add(time.Minute)}
	s.refresh = func(context.Context) (string, time.Duration, error) {
		if atomic.AddInt32(&callCount, 1) == 1 {
			close(entered)
			<-release
			// Simulates the in-flight call's view of the world as of
			// BEFORE Invalidate ran: it still sees (and would adopt/return)
			// the token that is about to be -- or already has been --
			// rejected.
			return "dead-token", time.Hour, nil
		}
		return "fresh-token", time.Hour, nil
	}

	type outcome struct {
		token string
		err   error
	}
	done := make(chan outcome, 1)
	go func() {
		token, err := s.Token(context.Background())
		done <- outcome{token, err}
	}()

	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("refresh never started")
	}

	s.Invalidate()
	close(release)

	var got outcome
	select {
	case got = <-done:
	case <-time.After(time.Second):
		t.Fatal("Token() never returned")
	}

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
// tests above.
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
