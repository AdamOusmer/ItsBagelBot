// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"

	"ItsBagelBot/internal/domain/automation"
	loyaltyrpc "ItsBagelBot/internal/domain/rpc/loyalty"
	"ItsBagelBot/internal/testnats"
	"ItsBagelBot/pkg/codec"

	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	globalWindowKey = "cooldown:auto:123:id-t"
	viewerWindowKey = "cooldown:autouser:123:999:id-t"
	aboveValkey     = 86_401
)

type fakeLongCooldowns struct {
	refuse     bool
	claimErr   error
	releaseErr error
	claimed    []string
	released   []string
}

func (s *fakeLongCooldowns) ClaimLongCooldowns(_ context.Context, claim LongCooldownClaim) (LongCooldownHold, bool, error) {
	hold := LongCooldownHold{BroadcasterID: claim.BroadcasterID}
	for _, window := range claim.Windows {
		s.claimed = append(s.claimed, window.Key)
		hold.Windows = append(hold.Windows, loyaltyrpc.CooldownWindow{Key: window.Key, ExpiresAtMS: 1})
	}
	if s.claimErr != nil {
		return LongCooldownHold{}, false, s.claimErr
	}
	return hold, !s.refuse, nil
}

func (s *fakeLongCooldowns) ReleaseLongCooldowns(ctx context.Context, hold LongCooldownHold) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	for _, window := range hold.Windows {
		s.released = append(s.released, window.Key)
	}
	return s.releaseErr
}

type valkeyOutcome struct {
	recordingCooldown
	err error
}

func (c *valkeyOutcome) Allow(ctx context.Context, key string, ttl time.Duration) (bool, error) {
	won, _ := c.recordingCooldown.Allow(ctx, key, ttl)
	return won, c.err
}

func runCooldown(t *testing.T, rig autoRig, params map[string]any) []string {
	t.Helper()
	rig.automations = []automation.Automation{testAutomation("t", "command", map[string]any{"name": "!t"}, cond("cooldown", params))}
	p, runs := rig.pipeline()
	process(t, p, chatEnv("!t", ""))
	return runs.summary()
}

func TestCooldownWindowsPickTheirStoreBySize(t *testing.T) {
	for name, tc := range map[string]struct {
		params     map[string]any
		wantValkey []string
		wantLong   []string
	}{
		"both at a day stay in valkey": {map[string]any{"global_s": 86_400, "user_s": 86_400}, []string{globalWindowKey, viewerWindowKey}, nil},
		"global above a day":           {map[string]any{"global_s": aboveValkey}, nil, []string{globalWindowKey}},
		"viewer above a day":           {map[string]any{"user_s": automation.MaxCooldownSeconds}, nil, []string{viewerWindowKey}},
		"both long, viewer first":      {map[string]any{"global_s": aboveValkey, "user_s": aboveValkey}, nil, []string{viewerWindowKey, globalWindowKey}},
		"long global, short viewer":    {map[string]any{"global_s": aboveValkey, "user_s": 60}, []string{viewerWindowKey}, []string{globalWindowKey}},
		"short global, long viewer":    {map[string]any{"global_s": 60, "user_s": aboveValkey}, []string{globalWindowKey}, []string{viewerWindowKey}},
	} {
		t.Run(name, func(t *testing.T) {
			short, long := &recordingCooldown{}, &fakeLongCooldowns{}

			summary := runCooldown(t, autoRig{cooldown: short, long: long}, tc.params)

			assert.Equal(t, []string{"t:ok"}, summary)
			assert.Equal(t, tc.wantValkey, short.keys)
			assert.Equal(t, tc.wantLong, long.claimed)
			assert.Empty(t, long.released)
		})
	}
}

func TestLongCooldownRefusalLeavesValkeyUnspent(t *testing.T) {
	short, long := &recordingCooldown{}, &fakeLongCooldowns{refuse: true}

	summary := runCooldown(t, autoRig{cooldown: short, long: long}, map[string]any{"global_s": aboveValkey, "user_s": 60})

	assert.Equal(t, []string{"t:skipped:condition:0:cooldown"}, summary)
	assert.Empty(t, short.keys)
	assert.Empty(t, long.released)
}

func TestValkeyRefusalReleasesLongCooldown(t *testing.T) {
	short, long := &recordingCooldown{deny: true}, &fakeLongCooldowns{}

	summary := runCooldown(t, autoRig{cooldown: short, long: long}, map[string]any{"global_s": aboveValkey, "user_s": 60})

	assert.Equal(t, []string{"t:skipped:condition:0:cooldown"}, summary)
	assert.Equal(t, []string{globalWindowKey}, long.released)
}

func TestCooldownStoreErrorsNeverPassTheCondition(t *testing.T) {
	boom := errors.New("store down")
	params := map[string]any{"global_s": aboveValkey, "user_s": 60}

	t.Run("valkey error releases the long row", func(t *testing.T) {
		long := &fakeLongCooldowns{}
		summary := runCooldown(t, autoRig{cooldown: &valkeyOutcome{err: boom}, long: long}, params)
		assert.NotEqual(t, []string{"t:ok"}, summary)
		assert.Equal(t, []string{globalWindowKey}, long.released)
	})
	t.Run("long store error skips valkey", func(t *testing.T) {
		short := &recordingCooldown{}
		summary := runCooldown(t, autoRig{cooldown: short, long: &fakeLongCooldowns{claimErr: boom}}, params)
		assert.NotEqual(t, []string{"t:ok"}, summary)
		assert.Empty(t, short.keys)
	})
	t.Run("failed release still refuses", func(t *testing.T) {
		long := &fakeLongCooldowns{releaseErr: boom}
		summary := runCooldown(t, autoRig{cooldown: &recordingCooldown{deny: true}, long: long}, params)
		assert.NotEqual(t, []string{"t:ok"}, summary)
	})
	t.Run("no long store refuses", func(t *testing.T) {
		short := &recordingCooldown{}
		summary := runCooldown(t, autoRig{cooldown: short}, params)
		assert.Equal(t, []string{"t:skipped:condition:0:cooldown"}, summary)
		assert.Empty(t, short.keys)
	})
}

func TestReleaseSurvivesACancelledRun(t *testing.T) {
	long := &fakeLongCooldowns{}
	p, _ := autoRig{cooldown: &recordingCooldown{deny: true}, long: long}.pipeline()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	won, err := p.claimCooldownWindows(ctx, 123, []CooldownWindow{{Key: "long", TTL: aboveValkey * time.Second}, {Key: "short", TTL: time.Minute}})

	require.NoError(t, err)
	assert.False(t, won)
	assert.Equal(t, []string{"long"}, long.released)
}

func TestMixedStoresAllOrNothingOnRealValkey(t *testing.T) {
	client := newHotPathTestClient(t)
	ctx := context.Background()
	long := &fakeLongCooldowns{}
	p, _ := autoRig{cooldown: NewValkeyCooldown(client), long: long}.pipeline()
	short := "test:cooldown:mixed:" + strconv.FormatInt(time.Now().UnixNano(), 10)
	t.Cleanup(func() { client.Do(ctx, client.B().Del().Key(short).Build()) })
	windows := []CooldownWindow{{Key: "long", TTL: aboveValkey * time.Second}, {Key: short, TTL: time.Minute}}

	won, err := p.claimCooldownWindows(ctx, 123, windows)
	require.NoError(t, err)
	require.True(t, won)
	assert.Empty(t, long.released)

	won, err = p.claimCooldownWindows(ctx, 123, windows)
	require.NoError(t, err)
	assert.False(t, won)
	assert.Equal(t, []string{"long"}, long.released)
}

func TestLoyaltyRPCCooldownSubjectsAndPayloads(t *testing.T) {
	nc := testnats.Connect(t)
	claimed := make(chan loyaltyrpc.Request, 1)
	_, err := nc.Subscribe("loy.cooldown.claim", func(msg *nats.Msg) {
		var req loyaltyrpc.Request
		assert.NoError(t, codec.Unmarshal(msg.Data, &req))
		claimed <- req
		body, err := codec.Marshal(loyaltyrpc.Reply{Claimed: true, Cooldowns: []loyaltyrpc.CooldownWindow{{Key: "k", ExpiresAtMS: 9}}})
		assert.NoError(t, err)
		assert.NoError(t, msg.Respond(body))
	})
	require.NoError(t, err)
	released := make(chan loyaltyrpc.Request, 1)
	_, err = nc.Subscribe("loy.cooldown.release", func(msg *nats.Msg) {
		var req loyaltyrpc.Request
		assert.NoError(t, codec.Unmarshal(msg.Data, &req))
		released <- req
		body, err := codec.Marshal(loyaltyrpc.Reply{Found: true})
		assert.NoError(t, err)
		assert.NoError(t, msg.Respond(body))
	})
	require.NoError(t, err)
	store := NewLoyaltyRPC(nc, "loy")
	ctx := context.Background()

	hold, won, err := store.ClaimLongCooldowns(ctx, LongCooldownClaim{BroadcasterID: 7, Windows: []CooldownWindow{{Key: "k", TTL: aboveValkey * time.Second}}})

	require.NoError(t, err)
	assert.True(t, won)
	assert.Equal(t, loyaltyrpc.Request{UserID: "7", Cooldowns: []loyaltyrpc.CooldownWindow{{Key: "k", TTLSeconds: aboveValkey}}}, <-claimed)

	require.NoError(t, store.ReleaseLongCooldowns(ctx, hold))
	assert.Equal(t, loyaltyrpc.Request{UserID: "7", Cooldowns: []loyaltyrpc.CooldownWindow{{Key: "k", ExpiresAtMS: 9}}}, <-released)
}

func TestLoyaltyRPCCooldownWithoutResponderIsAnError(t *testing.T) {
	store := NewLoyaltyRPC(testnats.Connect(t), "absent")

	_, won, err := store.ClaimLongCooldowns(context.Background(), LongCooldownClaim{BroadcasterID: 7, Windows: []CooldownWindow{{Key: "k", TTL: aboveValkey * time.Second}}})

	assert.Error(t, err)
	assert.False(t, won)
}
