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
