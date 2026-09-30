// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/internal/domain/event/lane"
	"ItsBagelBot/internal/projection"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type windowStore struct {
	mu   sync.Mutex
	open map[string]time.Duration
}

func newWindowStore() *windowStore {
	return &windowStore{open: map[string]time.Duration{}}
}

func (s *windowStore) Allow(ctx context.Context, key string, ttl time.Duration) (bool, error) {
	return s.AllowAll(ctx, []CooldownWindow{{Key: key, TTL: ttl}})
}

func (s *windowStore) AllowAll(_ context.Context, windows []CooldownWindow) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, w := range windows {
		if _, held := s.open[w.Key]; held {
			return false, nil
		}
	}
	for _, w := range windows {
		s.open[w.Key] = w.TTL
	}
	return true, nil
}

func (s *windowStore) elapse(d time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for key, left := range s.open {
		if left <= d {
			delete(s.open, key)
			continue
		}
		s.open[key] = left - d
	}
}

func cooldownCommand(global, perUser uint) projection.Command {
	return projection.Command{
		Name:         "hello",
		Aliases:      []string{"hi"},
		Response:     "hey",
		IsActive:     true,
		Perm:         "everyone",
		Cooldown:     global,
		UserCooldown: perUser,
	}
}

func cooldownPipeline(cmd projection.Command, cd CooldownStore) *Pipeline {
	d := Deps{
		Proj:     fakeReader{cmd: cmd, cmdFound: true},
		Live:     liveAlways{},
		Cooldown: cd,
		Pub:      &fakePublisher{},
		Log:      zap.NewNop(),
	}
	return NewPipeline(d, NewRegistry(zap.NewNop()), Config{OutgressPremium: premiumSubj, OutgressStandard: standardSubj})
}

type invocation struct {
	after   time.Duration
	channel uint64
	viewer  string
	text    string
	runs    bool
}

func (i invocation) context() *module.Context {
	channel := i.channel
	if channel == 0 {
		channel = 123
	}
	text := i.text
	if text == "" {
		text = "!hello"
	}
	env := lane.Envelope{
		Type:              chatType,
		Text:              text,
		BroadcasterUserID: strconv.FormatUint(channel, 10),
		ChatterUserID:     i.viewer,
		ChatterUserLogin:  "viewer" + i.viewer,
	}
	return &module.Context{Env: env, BroadcasterID: channel, Log: zap.NewNop()}
}

func TestCommandCooldownScopes(t *testing.T) {
	cases := []struct {
		name    string
		global  uint
		perUser uint
		steps   []invocation
	}{
		{
			name: "disabled", global: 0, perUser: 0,
			steps: []invocation{
				{viewer: "1", runs: true},
				{viewer: "1", runs: true},
				{viewer: "2", runs: true},
			},
		},
		{
			name: "global only", global: 5, perUser: 0,
			steps: []invocation{
				{viewer: "1", runs: true},
				{viewer: "2", runs: false},
				{after: 5 * time.Second, viewer: "1", runs: true},
			},
		},
		{
			name: "user only", global: 0, perUser: 60,
			steps: []invocation{
				{viewer: "1", runs: true},
				{viewer: "1", runs: false},
				{viewer: "2", runs: true},
				{after: 59 * time.Second, viewer: "1", runs: false},
				{after: time.Second, viewer: "1", runs: true},
			},
		},
		{
			name: "global and user", global: 5, perUser: 60,
			steps: []invocation{
				{viewer: "1", runs: true},
				{viewer: "2", runs: false},
				{after: 5 * time.Second, viewer: "1", runs: false},
				{viewer: "2", runs: true},
				{viewer: "3", runs: false},
				{after: 55 * time.Second, viewer: "1", runs: true},
			},
		},
		{
			name: "alias shares both windows", global: 5, perUser: 60,
			steps: []invocation{
				{viewer: "1", text: "!hi", runs: true},
				{viewer: "2", text: "!HELLO", runs: false},
				{after: 5 * time.Second, viewer: "1", runs: false},
				{viewer: "1", text: "!hi", runs: false},
				{viewer: "2", text: "!hi", runs: true},
			},
		},
		{
			name: "windows stay inside their channel", global: 5, perUser: 60,
			steps: []invocation{
				{viewer: "1", runs: true},
				{viewer: "1", channel: 456, runs: true},
				{viewer: "2", channel: 789, runs: true},
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := newWindowStore()
			p := cooldownPipeline(cooldownCommand(tc.global, tc.perUser), store)
			for i, step := range tc.steps {
				store.elapse(step.after)
				got, err := dispatch(t, p, step.context())
				require.NoError(t, err)
				assert.Equal(t, step.runs, len(got) == 1, "step %d: viewer %s ran %q", i, step.viewer, step.context().Env.Text)
			}
		})
	}
}

func TestCommandCooldownRejectionClaimsNothing(t *testing.T) {
	store := newWindowStore()
	p := cooldownPipeline(cooldownCommand(5, 60), store)

	_, err := dispatch(t, p, invocation{viewer: "1"}.context())
	require.NoError(t, err)
	store.elapse(5 * time.Second)
	got, err := dispatch(t, p, invocation{viewer: "1"}.context())
	require.NoError(t, err)
	require.Empty(t, got)

	assert.NotContains(t, store.open, CommandCooldownKey(123, "hello"), "a viewer blocked by their own window must not start the shared one")
}

func TestValkeyCooldownAllowAllIsAllOrNothing(t *testing.T) {
	client := newHotPathTestClient(t)
	ctx := context.Background()
	cd := NewValkeyCooldown(client)
	prefix := "test:cooldown:" + strconv.FormatInt(time.Now().UnixNano(), 10) + ":"
	shared := CooldownWindow{Key: prefix + "shared", TTL: time.Minute}
	viewer := CooldownWindow{Key: prefix + "viewer", TTL: time.Hour}
	t.Cleanup(func() { client.Do(ctx, client.B().Del().Key(shared.Key, viewer.Key).Build()) })

	ok, err := cd.Allow(ctx, viewer.Key, viewer.TTL)
	require.NoError(t, err)
	require.True(t, ok)

	ok, err = cd.AllowAll(ctx, []CooldownWindow{shared, viewer})
	require.NoError(t, err)
	assert.False(t, ok)
	exists, err := client.Do(ctx, client.B().Exists().Key(shared.Key).Build()).AsInt64()
	require.NoError(t, err)
	assert.Zero(t, exists, "a rejected claim must leave the free window untouched")

	require.NoError(t, client.Do(ctx, client.B().Del().Key(viewer.Key).Build()).Error())
	ok, err = cd.AllowAll(ctx, []CooldownWindow{shared, viewer})
	require.NoError(t, err)
	require.True(t, ok)
	for _, w := range []CooldownWindow{shared, viewer} {
		left, err := client.Do(ctx, client.B().Pttl().Key(w.Key).Build()).AsInt64()
		require.NoError(t, err)
		assert.InDelta(t, w.TTL.Milliseconds(), left, 5000, "window %s", w.Key)
	}
}

func TestValkeyCommandCooldownAcrossReplicas(t *testing.T) {
	client := newHotPathTestClient(t)
	ctx := context.Background()
	channel := uint64(time.Now().UnixNano())
	cmd := cooldownCommand(5, 60)
	replicas := []*Pipeline{
		cooldownPipeline(cmd, NewValkeyCooldown(client)),
		cooldownPipeline(cmd, NewValkeyCooldown(newHotPathTestClient(t))),
	}
	t.Cleanup(func() {
		keys := []string{CommandCooldownKey(channel, "hello")}
		for v := range 3 {
			keys = append(keys, gateRule{name: "hello"}.viewerCooldownKey(invocation{channel: channel, viewer: strconv.Itoa(v)}.context()))
		}
		client.Do(ctx, client.B().Del().Key(keys...).Build())
	})

	var ran atomic.Int32
	var wg sync.WaitGroup
	for i := range 40 {
		wg.Go(func() {
			text := []string{"!hello", "!hi"}[i%2]
			got, err := dispatch(t, replicas[i%2], invocation{channel: channel, viewer: strconv.Itoa(i % 3), text: text}.context())
			assert.NoError(t, err)
			ran.Add(int32(len(got)))
		})
	}
	wg.Wait()

	assert.Equal(t, int32(1), ran.Load(), "one invocation wins the shared window across replicas and aliases")
}
