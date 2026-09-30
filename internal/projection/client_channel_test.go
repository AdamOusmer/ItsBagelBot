// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package projection

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/valkey-io/valkey-go"
	"go.uber.org/zap"
)

// Gate reads before they reach the real wire client. Waiting for both gates
// proves foreground overlap without relying on timing or network scheduling.
type gatedProjectionReads struct {
	valkey.Client
	started chan string
	release <-chan struct{}
	reads   atomic.Int64
}

func (c *gatedProjectionReads) Do(ctx context.Context, cmd valkey.Completed) valkey.ValkeyResult {
	verb := cmd.Commands()[0]
	switch verb {
	case "HMGET", "HGETALL", "EVAL_RO":
		c.reads.Add(1)
		if c.started != nil {
			c.started <- verb
		}
		if c.release != nil {
			select {
			case <-c.release:
			case <-ctx.Done():
			}
		}
	}
	return c.Client.Do(ctx, cmd)
}

func channelTestClient(t *testing.T) (*Client, *gatedProjectionReads, *fakeValkey) {
	t.Helper()
	f := newFakeValkey(t)
	f.seed("settings:81", fakeField{"status", "premium"})
	f.seed("settings:81", fakeField{"active", "1"})
	f.seed("settings:81", fakeField{"locale", "fr"})
	f.seed("settings:81", fakeField{modulesMarkerField, "1"})
	f.seed("settings:81", fakeField{"module:automod:enabled", "1"})
	f.seed("settings:81", fakeField{"module:automod:revision", "7"})
	reads := &gatedProjectionReads{Client: f.client}
	c := NewClient(Config{Store: NewStore(reads), TTL: time.Minute, Log: zap.NewNop()})
	t.Cleanup(c.Close)
	return c, reads, f
}

func TestLoadChannelColdReadsOverlap(t *testing.T) {
	c, reads, _ := channelTestClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	release := make(chan struct{})
	defer close(release)
	reads.release = release
	reads.started = make(chan string, 2)
	done := make(chan struct{})
	var mods map[string]ModuleView
	var user User
	var err error
	go func() {
		mods, user, err = c.LoadChannel(ctx, 81, true)
		close(done)
	}()
	seen := make(map[string]bool)
	for range 2 {
		select {
		case verb := <-reads.started:
			seen[verb] = true
		case <-ctx.Done():
			t.Fatal("user and modules must start before either read completes")
		}
	}
	require.Equal(t, map[string]bool{"HMGET": true, "EVAL_RO": true}, seen)
	// Unblock both reads without closing twice in cleanup.
	release <- struct{}{}
	release <- struct{}{}
	<-done
	require.NoError(t, err)
	require.Equal(t, "fr", user.Locale)
	require.True(t, user.Premium())
	require.Equal(t, 7, mods["automod"].Revision)
	// A repeated load performs no additional wire read.
	_, _, err = c.LoadChannel(ctx, 81, true)
	require.NoError(t, err)
	require.EqualValues(t, 2, reads.reads.Load())
}

func TestLoadChannelIndependentInvalidationAndOptionalModules(t *testing.T) {
	c, reads, f := channelTestClient(t)
	ctx := context.Background()
	_, user, err := c.LoadChannel(ctx, 81, false)
	require.NoError(t, err)
	require.Equal(t, "fr", user.Locale)
	require.EqualValues(t, 1, reads.reads.Load(), "user-only loads must not read modules")
	mods, _, err := c.LoadChannel(ctx, 81, true)
	require.NoError(t, err)
	require.True(t, mods["automod"].IsEnabled)
	require.EqualValues(t, 2, reads.reads.Load())
	f.seed("settings:81", fakeField{"locale", "en"})
	c.evictScope("locale", 81, nil)
	mods, user, err = c.LoadChannel(ctx, 81, true)
	require.NoError(t, err)
	require.Equal(t, "en", user.Locale)
	require.True(t, mods["automod"].IsEnabled)
	require.EqualValues(t, 3, reads.reads.Load(), "locale invalidation reloads only User")
	f.seed("settings:81", fakeField{"module:automod:enabled", "0"})
	c.evictScope("modules", 81, nil)
	mods, user, err = c.LoadChannel(ctx, 81, true)
	require.NoError(t, err)
	require.False(t, mods["automod"].IsEnabled)
	require.Equal(t, "en", user.Locale)
	require.EqualValues(t, 4, reads.reads.Load(), "modules invalidation reloads only Modules")
}

func TestLoadChannelConcurrentColdLoadsSingleflight(t *testing.T) {
	c, reads, _ := channelTestClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	release := make(chan struct{})
	reads.release = release
	reads.started = make(chan string, 32)
	var wg sync.WaitGroup
	for range 16 {
		wg.Go(func() {
			mods, user, err := c.LoadChannel(ctx, 81, true)
			if err != nil {
				t.Errorf("channel load failed: %v", err)
			}
			if !user.Premium() {
				t.Errorf("channel user must retain premium state: %+v", user)
			}
			if mods["automod"].Revision != 7 {
				t.Errorf("channel modules must retain revision 7: %+v", mods)
			}
		})
	}
	for range 2 {
		select {
		case <-reads.started:
		case <-ctx.Done():
			close(release)
			wg.Wait()
			t.Fatal("cold reads did not start")
		}
	}
	close(release)
	wg.Wait()
	require.EqualValues(t, 2, reads.reads.Load(), "concurrent callers share both cache fills")
}

func TestLoadChannelFailurePolicies(t *testing.T) {
	c, _, _ := channelTestClient(t)
	ctx := context.Background()
	// An unavailable RPC serves the conservative user fallback for this call
	// only, while absent modules remain a retryable error; neither is cached.
	_, user, err := c.LoadChannel(ctx, 999, true)
	require.Error(t, err)
	require.Equal(t, User{Status: "standard"}, user)
	_, modulesCached := c.modules.Get(key("modules", 999))
	require.False(t, modulesCached)
	_, userCached := c.users.Get(key("user", 999))
	require.False(t, userCached)
}

func BenchmarkLoadChannelHot(b *testing.B) {
	c := NewClient(Config{Store: &Store{}, TTL: time.Minute, Log: zap.NewNop()})
	defer c.Close()
	c.users.Set(key("user", 81), User{Status: "standard", Locale: "en"})
	c.modules.Set(key("modules", 81), map[string]ModuleView{"automod": {Name: "automod", Revision: 7}})
	ctx := context.Background()
	for _, combined := range []bool{false, true} {
		name := "separate"
		if combined {
			name = "channel"
		}
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				if combined {
					_, _, _ = c.LoadChannel(ctx, 81, true)
				} else {
					_, _ = c.Modules(ctx, 81)
					_, _ = c.User(ctx, 81)
				}
			}
		})
	}
}
