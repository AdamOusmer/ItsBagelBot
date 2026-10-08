// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package projection

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
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

func seedChannel(f *fakeValkey) {
	for _, kv := range []fakeField{
		{"status", "premium"}, {"active", "1"}, {"locale", "fr"}, {modulesMarkerField, "1"},
		{"module:automod:enabled", "1"}, {"module:automod:revision", "7"},
	} {
		f.seed("settings:81", kv)
	}
}

func channelTestClient(t *testing.T) (*Client, *gatedProjectionReads, *fakeValkey) {
	t.Helper()
	f := newFakeValkey(t)
	seedChannel(f)
	reads := &gatedProjectionReads{Client: f.Client()}
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
	release <- struct{}{}
	release <- struct{}{}
	<-done
	require.NoError(t, err)
	require.Equal(t, "fr", user.Locale)
	require.True(t, user.Premium())
	require.Equal(t, 7, mods["automod"].Revision)
	_, _, err = c.LoadChannel(ctx, 81, true)
	require.NoError(t, err)
	require.EqualValues(t, 2, reads.reads.Load(), "a repeated load performs no additional wire read")
}

func TestLoadChannelInvalidatesUserAndModulesIndependently(t *testing.T) {
	f := newFakeValkey(t)
	seedChannel(f)
	reads := &gatedProjectionReads{Client: f.Client()}
	c, evict := invalidatedClient(t, NewStore(reads))
	ctx := context.Background()
	load := func() (map[string]ModuleView, User) {
		mods, user, err := c.LoadChannel(ctx, 81, true)
		require.NoError(t, err)
		return mods, user
	}

	_, user, err := c.LoadChannel(ctx, 81, false)
	require.NoError(t, err)
	require.Equal(t, "fr", user.Locale)
	require.EqualValues(t, 1, reads.reads.Load(), "user-only loads must not read modules")
	mods, _ := load()
	require.True(t, mods["automod"].IsEnabled)
	require.EqualValues(t, 2, reads.reads.Load())

	f.seed("settings:81", fakeField{"locale", "en"})
	evict("locale", 81)
	require.Eventually(t, func() bool { _, u := load(); return u.Locale == "en" }, 2*time.Second, 5*time.Millisecond)
	mods, _ = load()
	assert.True(t, mods["automod"].IsEnabled)
	assert.EqualValues(t, 3, reads.reads.Load(), "locale invalidation reloads only User")

	f.seed("settings:81", fakeField{"module:automod:enabled", "0"})
	evict("modules", 81)
	require.Eventually(t, func() bool { m, _ := load(); return !m["automod"].IsEnabled }, 2*time.Second, 5*time.Millisecond)
	_, user = load()
	assert.Equal(t, "en", user.Locale)
	assert.EqualValues(t, 4, reads.reads.Load(), "modules invalidation reloads only Modules")
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
	c, _, f := channelTestClient(t)
	ctx := context.Background()
	_, user, err := c.LoadChannel(ctx, 999, true)
	require.Error(t, err)
	require.Equal(t, User{Status: "standard"}, user)

	f.seed("settings:999", fakeField{"status", "paid"})
	f.seed("settings:999", fakeField{"active", "1"})
	f.seed("settings:999", fakeField{modulesMarkerField, "1"})
	f.seed("settings:999", fakeField{"module:automod:enabled", "1"})

	mods, user, err := c.LoadChannel(ctx, 999, true)
	require.NoError(t, err, "the failed modules load must not have been cached")
	assert.True(t, mods["automod"].IsEnabled)
	assert.Equal(t, "paid", user.Status, "the failed user load must not have been cached")
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
