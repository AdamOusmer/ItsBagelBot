// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package twitch

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func fakeTokenHTTP(t *testing.T, handler roundTripFunc) {
	t.Helper()
	orig := tokenHTTP
	tokenHTTP = &http.Client{Transport: handler}
	t.Cleanup(func() { tokenHTTP = orig })
}

func mintResponse(token string) roundTripFunc {
	return func(*http.Request) (*http.Response, error) {
		return respond(http.StatusOK, fmt.Sprintf(`{"access_token":%q,"refresh_token":"rotated","expires_in":14400}`, token)), nil
	}
}

func countMints(t *testing.T, token string) *atomic.Int32 {
	t.Helper()
	var mints atomic.Int32
	fakeTokenHTTP(t, func(req *http.Request) (*http.Response, error) {
		mints.Add(1)
		return mintResponse(token)(req)
	})
	return &mints
}

func storedIO(load func(context.Context) StoredLoad) StoredTokenIO {
	return StoredTokenIO{
		Load:    load,
		Persist: func(context.Context, string, string, time.Time) error { return nil },
	}
}

func staticLoad(stored StoredLoad) func(context.Context) StoredLoad {
	return func(context.Context) StoredLoad { return stored }
}

func TestStoredUserTokenSourceAdoption(t *testing.T) {
	future := time.Now().Add(2 * time.Hour)
	nearExpiry := time.Now().Add(refreshMargin - time.Second)
	tests := []struct {
		name      string
		stored    StoredLoad
		wantToken string
	}{
		{"adopts a stored token comfortably in the future", StoredLoad{AccessToken: "stored", AccessTokenExpiresAt: &future, RefreshToken: "r"}, "stored"},
		{"refuses a stored expiry inside the refresh margin", StoredLoad{AccessToken: "stored", AccessTokenExpiresAt: &nearExpiry}, ""},
		{"never reads a missing expiry as valid forever", StoredLoad{AccessToken: "stored"}, ""},
		{"refuses an expiry without an access token", StoredLoad{AccessTokenExpiresAt: &future}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			io := storedIO(staticLoad(tt.stored))
			io.Persist = func(context.Context, string, string, time.Time) error {
				t.Fatal("adopting a stored token, or having nothing to mint, must never persist")
				return nil
			}
			src := NewStoredUserTokenSource(ClientCredentials{}, "", io, MintLease{})

			token, err := src.Token(context.Background())

			if tt.wantToken == "" {
				require.ErrorIs(t, err, ErrNoRefreshToken)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantToken, token)
			assert.Greater(t, src.ExpiresIn(), refreshMargin)
		})
	}
}

type leaseProbe struct {
	mints     *atomic.Int32
	loads     atomic.Int32
	acquires  atomic.Int32
	persisted atomic.Bool
	released  chan struct{}
}

type leaseCase struct {
	name      string
	lease     func(p *leaseProbe) MintLease
	load      func(p *leaseProbe) StoredLoad
	wantToken string
	wantMints int32
	wantLoads int32
	wantFast  bool
	released  bool
	persists  bool
	noPersist bool
}

func holdingLease(held bool, unavailable bool) func(*leaseProbe) MintLease {
	return func(p *leaseProbe) MintLease {
		return MintLease{Acquire: func(context.Context) (func(), bool, bool) {
			if !held {
				return nil, false, unavailable
			}
			return func() { close(p.released) }, true, false
		}}
	}
}

func emptyStore(*leaseProbe) StoredLoad { return StoredLoad{} }

func winnerLeaseCases() []leaseCase {
	return []leaseCase{
		{
			name: "the winner mints exactly once, persists, then releases the lease", lease: holdingLease(true, false), load: emptyStore,
			wantToken: "winner-token", wantMints: 1, wantLoads: -1, released: true, persists: true,
		},
		{
			name: "the loser acquires the lease when the winner vanishes",
			lease: func(p *leaseProbe) MintLease {
				return MintLease{Acquire: func(ctx context.Context) (func(), bool, bool) {
					if p.acquires.Add(1) == 1 {
						return nil, false, false
					}
					return holdingLease(true, false)(p).Acquire(ctx)
				}}
			},
			load: emptyStore, wantToken: "acquired-by-loser", wantMints: 1, wantLoads: -1, released: true,
		},
		{
			name: "an unavailable backend skips the wait entirely", lease: holdingLease(false, true), load: emptyStore,
			wantToken: "unavailable-fallback-mint", wantMints: 1, wantLoads: 1, wantFast: true,
		},
		{
			name: "an absent lease mints uncoordinated", lease: func(*leaseProbe) MintLease { return MintLease{} }, load: emptyStore,
			wantToken: "uncoordinated-mint", wantMints: 1, wantLoads: -1,
		},
	}
}

func loserLeaseCases() []leaseCase {
	future := time.Now().Add(time.Hour)
	return []leaseCase{
		{
			name:  "the loser adopts the winner's token without minting",
			lease: holdingLease(false, false),
			load: func(p *leaseProbe) StoredLoad {
				if p.loads.Load() < 3 {
					return StoredLoad{RefreshToken: "stored-refresh-token"}
				}
				return StoredLoad{AccessToken: "winners-token", AccessTokenExpiresAt: &future, RefreshToken: "stored-refresh-token"}
			},
			wantToken: "winners-token", wantMints: 0, wantLoads: -1, noPersist: true,
		},
		{
			name: "the loser mints as a fallback when nothing appears", lease: holdingLease(false, false),
			load:      func(*leaseProbe) StoredLoad { return StoredLoad{RefreshToken: "stored-refresh-token"} },
			wantToken: "fallback-mint", wantMints: 1, wantLoads: 1 + leaseWaitAttempts,
		},
	}
}

func (tt leaseCase) assert(t *testing.T, p *leaseProbe, token string, elapsed time.Duration) {
	t.Helper()
	assert.Equal(t, tt.wantToken, token)
	assert.Equal(t, tt.wantMints, p.mints.Load())
	if tt.wantLoads >= 0 {
		assert.Equal(t, tt.wantLoads, p.loads.Load())
	}
	if tt.wantFast {
		assert.Less(t, elapsed, leaseWaitInterval, "an unavailable lease backend must not pay the wait budget")
	}
	if tt.released {
		require.Eventually(t, func() bool { return isClosed(p.released) }, time.Second, time.Millisecond, "the lease must be released after the mint")
	}
	if tt.persists {
		require.Eventually(t, p.persisted.Load, time.Second, time.Millisecond, "the winner must persist its token")
	}
	if tt.noPersist {
		assert.False(t, p.persisted.Load(), "a loser never minted, so it must never persist")
	}
}

func TestMintLeaseCoordination(t *testing.T) {
	for _, tt := range append(winnerLeaseCases(), loserLeaseCases()...) {
		t.Run(tt.name, func(t *testing.T) {
			p := &leaseProbe{mints: countMints(t, tt.wantToken), released: make(chan struct{})}
			io := storedIO(func(context.Context) StoredLoad {
				p.loads.Add(1)
				return tt.load(p)
			})
			io.Persist = func(context.Context, string, string, time.Time) error { p.persisted.Store(true); return nil }
			src := NewStoredUserTokenSource(ClientCredentials{}, "seed-refresh", io, tt.lease(p))

			start := time.Now()
			token, err := src.Token(context.Background())

			require.NoError(t, err)
			tt.assert(t, p, token, time.Since(start))
		})
	}
}

func isClosed(ch <-chan struct{}) bool {
	select {
	case <-ch:
		return true
	default:
		return false
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

func TestInvalidateDuringInFlightRefreshDoesNotResurrectDeadToken(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	var callCount int32
	s := newInFlightRaceSource(entered, release, &callCount)
	type outcome struct {
		token string
		err   error
	}
	done := make(chan outcome, 1)
	go func() {
		token, err := s.Token(context.Background())
		done <- outcome{token, err}
	}()
	<-entered

	s.Invalidate()
	close(release)
	got := <-done

	require.NoError(t, got.err)
	assert.Equal(t, "fresh-token", got.token, "the stale result is discarded and one bounded retry runs")
	assert.EqualValues(t, 2, atomic.LoadInt32(&callCount))
	again, err := s.Token(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "fresh-token", again, "the Source caches the fresh token, not the resurrected one")
	assert.EqualValues(t, 2, atomic.LoadInt32(&callCount))
}

func TestInvalidateForcesMintPastStillValidStoredToken(t *testing.T) {
	mints := countMints(t, "minted-after-unauthorized")
	expiresAt := time.Now().Add(2 * time.Hour)
	src := NewStoredUserTokenSource(ClientCredentials{}, "seed-refresh-token", storedIO(staticLoad(StoredLoad{
		AccessToken: "dead-access-token", AccessTokenExpiresAt: &expiresAt, RefreshToken: "stored-refresh-token",
	})), MintLease{})

	first, err := src.Token(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "dead-access-token", first, "the stored token is adopted first")
	assert.Zero(t, mints.Load())

	src.Invalidate()
	second, err := src.Token(context.Background())

	require.NoError(t, err)
	assert.Equal(t, "minted-after-unauthorized", second, "a fresh mint, not the store's stale value")
	assert.EqualValues(t, 1, mints.Load())
}

func TestLoserAfterInvalidateDoesNotAdoptSameDeadToken(t *testing.T) {
	mints := countMints(t, "minted-by-loser")
	future := time.Now().Add(time.Hour)
	var loads atomic.Int32
	lease := MintLease{Acquire: func(context.Context) (func(), bool, bool) { return nil, false, false }}
	src := NewStoredUserTokenSource(ClientCredentials{}, "seed-refresh-token", storedIO(func(context.Context) StoredLoad {
		loads.Add(1)
		return StoredLoad{AccessToken: "dead-access-token", AccessTokenExpiresAt: &future, RefreshToken: "stored-refresh-token"}
	}), lease)

	first, err := src.Token(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "dead-access-token", first)

	src.Invalidate()
	second, err := src.Token(context.Background())

	require.NoError(t, err)
	assert.Equal(t, "minted-by-loser", second, "the loser must not re-adopt the token that was just invalidated")
	assert.EqualValues(t, 1, mints.Load())
	assert.EqualValues(t, 2+leaseWaitAttempts, loads.Load())
}

func TestSkipAdoptSurvivesFailedMintAfterInvalidate(t *testing.T) {
	var mintShouldFail atomic.Bool
	mintShouldFail.Store(true)
	fakeTokenHTTP(t, func(req *http.Request) (*http.Response, error) {
		if mintShouldFail.Load() {
			return nil, errors.New("id.twitch.tv unreachable")
		}
		return mintResponse("finally-minted")(req)
	})
	expiresAt := time.Now().Add(2 * time.Hour)
	src := NewStoredUserTokenSource(ClientCredentials{}, "seed-refresh-token", storedIO(staticLoad(StoredLoad{
		AccessToken: "dead-access-token", AccessTokenExpiresAt: &expiresAt, RefreshToken: "stored-refresh-token",
	})), MintLease{})
	_, err := src.Token(context.Background())
	require.NoError(t, err, "priming adopts the stored token")

	src.Invalidate()
	_, err = src.Token(context.Background())
	require.Error(t, err, "the mint failure surfaces")
	_, err = src.Token(context.Background())
	require.Error(t, err, "the dead token is still refused")

	mintShouldFail.Store(false)
	token, err := src.Token(context.Background())

	require.NoError(t, err)
	assert.Equal(t, "finally-minted", token)
}

func TestTokenDoesNotHoldStateLockDuringRefresh(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
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
	require.NoError(t, <-done)
}

func TestConcurrentTokenRefreshIsCollapsed(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	s := &Source{refresh: func(context.Context) (string, time.Duration, error) {
		calls.Add(1)
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
		require.NoError(t, <-done)
	}

	assert.EqualValues(t, 1, calls.Load())
}

func TestConcurrentGenerationsDoNotRaceOnCurrentRefresh(t *testing.T) {
	entered, holdFirst := make(chan struct{}), make(chan struct{})
	var requests atomic.Int32
	fakeTokenHTTP(t, func(req *http.Request) (*http.Response, error) {
		if requests.Add(1) == 1 {
			close(entered)
			<-holdFirst
			return mintResponse("gen0-token")(req)
		}
		return mintResponse("gen1-token")(req)
	})
	lease := MintLease{Acquire: func(context.Context) (func(), bool, bool) { return func() {}, true, false }}
	s := NewStoredUserTokenSource(ClientCredentials{}, "seed-refresh", storedIO(staticLoad(StoredLoad{})), lease)
	done := make(chan struct{}, 2)
	token := func() {
		_, _ = s.Token(context.Background())
		done <- struct{}{}
	}
	go token()
	<-entered

	s.Invalidate()
	go token()
	require.Eventually(t, func() bool { return requests.Load() == 2 }, time.Second, time.Millisecond, "the new generation mints without waiting for the held one")
	close(holdFirst)

	for range 2 {
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("a Token() call never returned")
		}
	}
	assert.EqualValues(t, 2, requests.Load(), "one mint per generation")
}
