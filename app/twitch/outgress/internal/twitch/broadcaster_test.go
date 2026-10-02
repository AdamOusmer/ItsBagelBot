// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package twitch

import (
	"context"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func countingBuild(calls *int) func(string) *Source {
	return func(string) *Source {
		*calls++
		return &Source{}
	}
}

func TestBroadcasterTokensGet(t *testing.T) {
	var calls int
	b := NewBroadcasterTokens(countingBuild(&calls))
	var nilTokens *BroadcasterTokens

	first := b.Get("chan-a")

	assert.NotNil(t, first)
	assert.Same(t, first, b.Get("chan-a"), "a cache hit returns the same Source")
	assert.Equal(t, 1, calls, "build runs once per broadcaster")
	assert.NotSame(t, first, b.Get("chan-b"), "a distinct broadcaster gets its own Source")
	assert.Equal(t, 2, calls)
	assert.Nil(t, nilTokens.Get("chan-a"), "a nil receiver has no Source")
	assert.Nil(t, b.Get(""), "an empty id has no Source")
}

func TestBroadcasterTokensEvictTheLeastRecentlyUsedAtCapacity(t *testing.T) {
	var calls int
	b := NewBroadcasterTokens(countingBuild(&calls))
	for i := range maxBroadcasterSources {
		b.Get(strconv.Itoa(i))
	}
	require.Equal(t, maxBroadcasterSources, calls)

	b.Get("0")
	b.Get("overflow")
	b.Get("0")
	assert.Equal(t, maxBroadcasterSources+1, calls, "the touched entry survives the overflow insert")

	b.Get("1")
	assert.Equal(t, maxBroadcasterSources+2, calls, "the least recently used entry was evicted and is rebuilt")
}

func nearExpirySource(fn func(context.Context) (string, time.Duration, error)) *Source {
	s := &Source{refresh: fn}
	s.mu.Lock()
	s.token = "stale"
	s.expires = time.Now().Add(refreshMargin - time.Second)
	s.mu.Unlock()
	return s
}

func TestSweepOnceLeavesHealthySourceAlone(t *testing.T) {
	var calls int32
	b := NewBroadcasterTokens(func(string) *Source {
		s := &Source{refresh: func(context.Context) (string, time.Duration, error) {
			atomic.AddInt32(&calls, 1)
			return "fresh", time.Hour, nil
		}}
		s.mu.Lock()
		s.token = "healthy"
		s.expires = time.Now().Add(2 * time.Hour)
		s.mu.Unlock()
		return s
	})
	b.Get("chan-a")

	b.sweepOnce(context.Background())

	if got := atomic.LoadInt32(&calls); got != 0 {
		t.Fatalf("refresh calls = %d, want 0 for a healthy token", got)
	}
}

func TestSweepOnceDoesNotExtendLastUsed(t *testing.T) {
	b := NewBroadcasterTokens(func(string) *Source {
		return nearExpirySource(func(context.Context) (string, time.Duration, error) {
			return "fresh", time.Hour, nil
		})
	})
	b.Get("chan-a")
	old := time.Now().Add(-sourceIdleTTL - time.Minute)
	b.cache["chan-a"].lastUsed = old

	b.sweepOnce(context.Background())

	if got := b.cache["chan-a"].lastUsed; !got.Equal(old) {
		t.Fatalf("sweep changed lastUsed to %v, want unchanged %v", got, old)
	}
}

func TestSweepOnceDoesNotHoldLockDuringRefresh(t *testing.T) {
	var b *BroadcasterTokens
	b = NewBroadcasterTokens(func(string) *Source {
		return nearExpirySource(func(context.Context) (string, time.Duration, error) {
			b.Get("other-chan")
			return "fresh", time.Hour, nil
		})
	})
	b.Get("chan-a")

	done := make(chan struct{})
	go func() {
		b.sweepOnce(context.Background())
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("sweepOnce deadlocked: b.mu was still held while refresh ran")
	}
}

func mintingSource(calls *int32) *Source {
	return &Source{refresh: func(context.Context) (string, time.Duration, error) {
		atomic.AddInt32(calls, 1)
		return "fresh", time.Hour, nil
	}}
}

const sweepPassesUnderTest = 3

func TestSweepRefreshesOnlyLiveTokens(t *testing.T) {
	cases := []struct {
		name      string
		token     string
		expiresIn time.Duration
		wantCalls int32
	}{
		{name: "grant revoked or never given", token: "", expiresIn: refreshMargin - time.Second, wantCalls: 0},
		{name: "token already expired", token: "lapsed", expiresIn: -time.Minute, wantCalls: 0},
		{name: "live token inside the margin", token: "stale", expiresIn: refreshMargin - time.Second, wantCalls: 1},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var calls int32
			s := mintingSource(&calls)
			s.token = tc.token
			s.expires = time.Now().Add(tc.expiresIn)

			b := NewBroadcasterTokens(func(string) *Source { return s })
			b.Get("chan-a")
			for range sweepPassesUnderTest {
				b.sweepOnce(context.Background())
			}

			got := atomic.LoadInt32(&calls)
			if got != tc.wantCalls {
				t.Fatalf("refresh calls across %d passes = %d, want %d", sweepPassesUnderTest, got, tc.wantCalls)
			}
		})
	}
}

func TestSweepTickEvictsIdleSourceBelowCap(t *testing.T) {
	b := NewBroadcasterTokens(func(string) *Source { return mintingSource(new(int32)) })
	b.Get("idle")
	b.Get("active")
	b.cache["idle"].lastUsed = time.Now().Add(-sourceIdleTTL - time.Minute)

	b.sweepTick(context.Background())

	if len(b.cache) >= maxBroadcasterSources {
		t.Fatalf("cache size %d is at the cap, so this proves nothing about the TTL", len(b.cache))
	}
	if _, ok := b.cache["idle"]; ok {
		t.Error("source idle past sourceIdleTTL survived a sweep tick")
	}
	if _, ok := b.cache["active"]; !ok {
		t.Error("source used within sourceIdleTTL was evicted by a sweep tick")
	}
}
