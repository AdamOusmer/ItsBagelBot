// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"testing"
	"time"

	loyaltyrpc "ItsBagelBot/internal/domain/rpc/loyalty"
	"ItsBagelBot/internal/projection"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestSanitizeVarStripsControlChars(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"plain arg untouched", "hello world", "hello world"},
		{"leading slashes trimmed", "//ban everyone", "ban everyone"},
		{"newline verb dropped", "hi\n/ban everyone", "hi/ban everyone"},
		{"newline then slash-run", "\n\n/ban everyone", "ban everyone"},
		{"crlf smuggling", "hi\r\n/timeout @user", "hi/timeout @user"},
		{"tab-padded verb", "\t/announce raid", "announce raid"},
		{"carriage return alone", "a\rb", "ab"},
		{"nul byte dropped", "a\x00b", "ab"},
		{"all C0 controls", "\x01\x02\x1f x", "x"},
		{"del byte dropped", "a\x7fb", "ab"},
		{"mid-text url keeps slashes", "see https://example.com/x", "see https://example.com/x"},
		{"emoji survive", "café ☕ 🥯", "café ☕ 🥯"},
		{"empty stays empty", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, sanitizeVar(tt.in))
		})
	}
}

func TestWatchTickIdentityStableWithinBucketDistinctAcross(t *testing.T) {
	base := time.Unix(1_800_000_000, 0)
	sameMoment := base.Add(37 * time.Second)
	nextTick := base.Add(watchTickInterval)

	a := watchTickIdentity(42, base)
	assert.Equal(t, a, watchTickIdentity(42, sameMoment), "same bucket must collide")
	assert.NotEqual(t, a, watchTickIdentity(42, nextTick), "the next tick must earn again")
	assert.NotEqual(t, a, watchTickIdentity(43, base), "channels are independent")
}

func TestWatchTickGuardCollapsesRefire(t *testing.T) {
	store := newRecordingStore()
	d := NewEventDedup(store, "sesame:seen:", time.Hour, zap.NewNop())
	ctx := context.Background()

	ref := EffectRef{Identity: watchTickIdentity(42, time.Unix(1_800_000_000, 0)) + ":7", Effect: EffectEarn}
	assert.False(t, d.Duplicate(ctx, ref), "first fire applies")
	assert.True(t, d.Duplicate(ctx, ref), "re-fired tick is recognized")
}

type countingLoyalty struct {
	LoyaltyStore
	bumps     int
	peekCalls int
	value     int64
}

func (l *countingLoyalty) CounterBump(_ context.Context, _ CounterBump) (int64, error) {
	l.bumps++
	l.value++
	return l.value, nil
}

func (l *countingLoyalty) CounterPeek(context.Context, CounterTarget) (loyaltyrpc.Counter, bool, error) {
	l.peekCalls++
	return loyaltyrpc.Counter{Name: "deaths", Value: l.value}, true, nil
}

func TestCounterTokenNeverBumpsOnRedelivery(t *testing.T) {
	store := newRecordingStore()
	pub := &rawPublisher{}
	loyal := &countingLoyalty{}
	reader := fakeReader{cmd: projection.Command{
		Name: "foo", Response: "died {counter:deaths}", IsActive: true,
	}, cmdFound: true}

	d := Deps{
		Proj: reader, Live: liveAlways{}, Cooldown: NoopCooldown{},
		Loyalty: loyal, Pub: pub, Log: zap.NewNop(),
		Dedup: NewEventDedup(store, "sesame:seen:", time.Minute, zap.NewNop()),
	}
	p := NewPipeline(d, NewRegistry(zap.NewNop()), Config{
		OutgressPremium: premiumSubj, OutgressStandard: standardSubj, CountUses: true,
	})
	defer p.Close()

	require.NoError(t, p.Process(commandMsg(t, "m1", "!foo")))
	require.NoError(t, p.Process(commandMsg(t, "m1", "!foo")))
	require.NoError(t, p.Process(commandMsg(t, "m2", "!foo")))

	assert.Zero(t, loyal.bumps, "a template read never bumps, replayed or not")
	assert.Equal(t, 3, loyal.peekCalls, "every render re-peeks; a read has nothing to deduplicate")
	for _, key := range store.keys() {
		assert.NotContains(t, key, "cbump:deaths", "no read claims the counter-bump namespace")
	}
}
