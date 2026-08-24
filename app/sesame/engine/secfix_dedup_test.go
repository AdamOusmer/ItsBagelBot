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

// --- sanitizeVar: the newline-smuggling fix ---

// A viewer-supplied {args}/{touser} must never be able to mint per-line
// slash-verbs: emitResponse splits the expanded response on "\n" and routes
// each line through Translate independently, so an embedded newline plus a
// leading slash was a remote moderation verb executed as the bot.
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
		{"all C0 controls", "\x01\x02\x1f x", "x"}, // controls stripped, then the leading space run trims
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

// --- watch tick: stable per-bucket identity ---

// The identity must be byte-stable across replicas firing the same expiry,
// yet differ for the NEXT legitimate tick one interval later — otherwise the
// guard either misses replays or suppresses every second accrual.
func TestWatchTickIdentityStableWithinBucketDistinctAcross(t *testing.T) {
	base := time.Unix(1_800_000_000, 0)
	sameMoment := base.Add(37 * time.Second) // another replica's clock, same bucket
	nextTick := base.Add(watchTickInterval)

	a := watchTickIdentity(42, base)
	assert.Equal(t, a, watchTickIdentity(42, sameMoment), "same bucket must collide")
	assert.NotEqual(t, a, watchTickIdentity(42, nextTick), "the next tick must earn again")
	assert.NotEqual(t, a, watchTickIdentity(43, base), "channels are independent")
}

// End-to-end shape of the guard: claiming the same tick identity twice
// reports a duplicate, so a re-fired tick's chatter is paid exactly once.
func TestWatchTickGuardCollapsesRefire(t *testing.T) {
	store := newRecordingStore()
	d := NewEventDedup(store, "sesame:seen:", time.Hour, zap.NewNop())
	ctx := context.Background()

	ref := EffectRef{Identity: watchTickIdentity(42, time.Unix(1_800_000_000, 0)) + ":7", Effect: EffectEarn}
	assert.False(t, d.Duplicate(ctx, ref), "first fire applies")
	assert.True(t, d.Duplicate(ctx, ref), "re-fired tick is recognized")
}

// --- custom-command counter tokens: claim + peek on the dispatch path ---

// countingLoyalty counts bumps and serves the current value to peeks. The
// embedded nil interface covers the methods these tests never touch.
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

func (l *countingLoyalty) CounterPeek(context.Context, uint64, string, uint64, string) (loyaltyrpc.Counter, bool, error) {
	l.peekCalls++
	return loyaltyrpc.Counter{Name: "deaths", Value: l.value}, true, nil
}

// A redelivered command line must bump its {counter} token ONCE and render the
// peeked value on the replay, while a DISTINCT event bumps again.
func TestCounterTokenBumpReplayRendersPeekNotDoubleBump(t *testing.T) {
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
	require.NoError(t, p.Process(commandMsg(t, "m1", "!foo"))) // replay: same msg_id
	require.NoError(t, p.Process(commandMsg(t, "m2", "!foo"))) // distinct event

	// The claim lives under the CounterEffect namespace, the exact key
	// CounterClaim hands a folded claim+increment bump.
	require.Contains(t, store.keys(), "m1:cbump:deaths")
	assert.Equal(t, 2, loyal.bumps, "replay must not re-bump; distinct event must")
	assert.Equal(t, 1, loyal.peekCalls, "exactly one replay rendered the peeked value")
}
