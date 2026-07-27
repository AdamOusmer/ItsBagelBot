package engine

import (
	"context"
	"errors"
	"testing"
	"time"

	"ItsBagelBot/app/sesame/module"
	"ItsBagelBot/internal/domain/event/data"
	"ItsBagelBot/internal/domain/event/lane"
	loyaltyrpc "ItsBagelBot/internal/domain/rpc/loyalty"

	"github.com/bytedance/sonic"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// --- reply decoding: the array contract, pure ---

func TestSplitBumpReply(t *testing.T) {
	value, flag, err := splitBumpReply([]int64{42, 1}, nil)
	require.NoError(t, err)
	assert.EqualValues(t, 42, value)
	assert.EqualValues(t, 1, flag)

	// A decode error passes straight through (the Valkey-nil / fault paths).
	_, _, err = splitBumpReply(nil, errors.New("boom"))
	require.Error(t, err)

	// Wrong arity is a hard error: the script must always answer {value, flag}.
	_, _, err = splitBumpReply([]int64{7}, nil)
	require.Error(t, err)
	_, _, err = splitBumpReply([]int64{7, 1, 9}, nil)
	require.Error(t, err)
}

func TestOutcomeFor(t *testing.T) {
	applied := outcomeFor(5, bumpFlagApplied)
	assert.Equal(t, bumpOnceOutcome{value: 5, applied: true}, applied)

	warmDup := outcomeFor(5, bumpFlagWarmDup)
	assert.Equal(t, bumpOnceOutcome{value: 5, applied: false}, warmDup)

	dupCold := outcomeFor(0, bumpFlagDupCold)
	assert.Equal(t, bumpOnceOutcome{value: 0, dupCold: true}, dupCold)
}

// --- the seed-retry runner, driven with fake exec/loadSeed closures ---

func TestRunBumpOnceWarmApplies(t *testing.T) {
	seedCalls := 0
	out, err := runBumpOnce(
		func() int64 { seedCalls++; return 0 },
		func(seed string) (int64, int64, error) {
			assert.Equal(t, "", seed, "a warm bump probes without a seed")
			return 6, bumpFlagApplied, nil
		},
	)
	require.NoError(t, err)
	assert.Equal(t, bumpOnceOutcome{value: 6, applied: true}, out)
	assert.Zero(t, seedCalls, "a warm counter never loads a seed")
}

func TestRunBumpOnceWarmDuplicate(t *testing.T) {
	out, err := runBumpOnce(
		func() int64 { t.Fatal("no seed on a warm duplicate"); return 0 },
		func(string) (int64, int64, error) { return 9, bumpFlagWarmDup, nil },
	)
	require.NoError(t, err)
	assert.Equal(t, bumpOnceOutcome{value: 9, applied: false}, out)
}

func TestRunBumpOnceDupCold(t *testing.T) {
	out, err := runBumpOnce(
		func() int64 { t.Fatal("no seed on a dup-cold"); return 0 },
		func(string) (int64, int64, error) { return 0, bumpFlagDupCold, nil },
	)
	require.NoError(t, err)
	assert.True(t, out.dupCold)
	assert.False(t, out.applied)
}

func TestRunBumpOnceColdSeedsAndReExecs(t *testing.T) {
	var seeds []string
	seedCalls := 0
	out, err := runBumpOnce(
		func() int64 { seedCalls++; return 40 },
		func(seed string) (int64, int64, error) {
			seeds = append(seeds, seed)
			if seed == "" {
				return 0, bumpFlagNeedSeed, nil // cold: signal the caller to seed
			}
			return 41, bumpFlagApplied, nil // re-exec with the loaded seed applies
		},
	)
	require.NoError(t, err)
	assert.Equal(t, bumpOnceOutcome{value: 41, applied: true}, out)
	assert.Equal(t, 1, seedCalls, "the seed is loaded exactly once")
	assert.Equal(t, []string{"", "40"}, seeds, "probe then re-exec with the seed")
}

func TestRunBumpOnceProbeError(t *testing.T) {
	_, err := runBumpOnce(
		func() int64 { t.Fatal("no seed after a probe fault"); return 0 },
		func(string) (int64, int64, error) { return 0, 0, errors.New("down") },
	)
	require.Error(t, err)
}

func TestRunBumpOnceReExecError(t *testing.T) {
	_, err := runBumpOnce(
		func() int64 { return 3 },
		func(seed string) (int64, int64, error) {
			if seed == "" {
				return 0, bumpFlagNeedSeed, nil
			}
			return 0, 0, errors.New("down mid-seed")
		},
	)
	require.Error(t, err)
}

// --- settle: reporter-gating and fail-open, over a real reporter ---

// settleStore builds a store carrying only what settleBumpOnce touches: a real
// reporter over a capturing publisher, so "fired exactly once" is observable.
func settleStore(pub *rawPublisher) *ValkeyLoyaltyStore {
	return &ValkeyLoyaltyStore{
		reporter: NewLoyaltyReporter(pub, zap.NewNop()),
		log:      zap.NewNop(),
	}
}

func channelTarget() bumpOnceTarget {
	return bumpOnceTarget{broadcasterID: 123, name: "deaths", scope: data.CounterScopeChannel, delta: 1}
}

// reporterBumps flushes the reporter and returns the summed counter deltas.
func reporterBumps(t *testing.T, s *ValkeyLoyaltyStore, pub *rawPublisher) []data.CounterBumpEntry {
	t.Helper()
	s.reporter.Close()
	msgs := pub.payloads[data.SubjectLoyaltyCounters]
	if len(msgs) == 0 {
		return nil
	}
	require.Len(t, msgs, 1)
	var dto data.CounterBumpedDTO
	require.NoError(t, sonic.Unmarshal(msgs[0], &dto))
	return dto.Bumps
}

func TestSettleBumpOnceAppliedFiresReporter(t *testing.T) {
	pub := &rawPublisher{}
	s := settleStore(pub)

	value, applied, err := s.settleBumpOnce(channelTarget(), Viewer{}, bumpOnceOutcome{value: 7, applied: true}, nil)
	require.NoError(t, err)
	assert.True(t, applied)
	assert.EqualValues(t, 7, value)

	bumps := reporterBumps(t, s, pub)
	require.Len(t, bumps, 1, "an applied bump reports the summed delta once")
	assert.EqualValues(t, 1, bumps[0].Delta)
}

func TestSettleBumpOnceWarmDuplicateSkipsReporter(t *testing.T) {
	pub := &rawPublisher{}
	s := settleStore(pub)

	value, applied, err := s.settleBumpOnce(channelTarget(), Viewer{}, bumpOnceOutcome{value: 7, applied: false}, nil)
	require.NoError(t, err)
	assert.False(t, applied)
	assert.EqualValues(t, 7, value, "a warm duplicate still renders the current value")

	assert.Empty(t, reporterBumps(t, s, pub), "a duplicate must not re-apply the summed delta")
}

func TestSettleBumpOnceDupColdSignalsPeek(t *testing.T) {
	pub := &rawPublisher{}
	s := settleStore(pub)

	_, applied, err := s.settleBumpOnce(channelTarget(), Viewer{}, bumpOnceOutcome{dupCold: true}, nil)
	require.ErrorIs(t, err, errCounterDupCold)
	assert.False(t, applied)

	assert.Empty(t, reporterBumps(t, s, pub), "a dup-cold replay is still a duplicate: no re-apply")
}

func TestSettleBumpOnceFaultFailsOpen(t *testing.T) {
	pub := &rawPublisher{}
	s := settleStore(pub)

	fault := errors.New("valkey down")
	_, applied, err := s.settleBumpOnce(channelTarget(), Viewer{}, bumpOnceOutcome{}, fault)
	require.ErrorIs(t, err, fault)
	assert.True(t, applied, "a fault fails open: the accrual is applied once")
	assert.EqualValues(t, 1, s.bumpFailOpen.Load())

	bumps := reporterBumps(t, s, pub)
	require.Len(t, bumps, 1, "the durable accrual is not dropped on a Valkey outage")
	assert.EqualValues(t, 1, bumps[0].Delta)
}

// --- EventDedup.CounterClaim: the exact folded-claim key ---

func chatEnv(msgID string) lane.Envelope {
	return lane.Envelope{Type: chatType, MsgID: msgID, BroadcasterUserID: "123", ChatterUserID: "999"}
}

func TestCounterClaimActive(t *testing.T) {
	d := NewEventDedup(newRecordingStore(), "sesame:seen:", time.Minute, zap.NewNop())
	env := chatEnv("evt-1")

	key, ttl := d.CounterClaim(&env, "Deaths")
	assert.Equal(t, "sesame:seen:evt-1:"+CounterEffect("Deaths"), key,
		"the folded claim must name the exact key ValkeyStore.Seen would write")
	assert.Equal(t, "sesame:seen:evt-1:cbump:deaths", key)
	assert.Equal(t, time.Minute, ttl)
}

func TestCounterClaimInactive(t *testing.T) {
	// Kill switch: a nil guard writes no claim.
	var nilGuard *EventDedup
	key, ttl := nilGuard.CounterClaim(&lane.Envelope{MsgID: "evt-1"}, "deaths")
	assert.Empty(t, key)
	assert.Zero(t, ttl)

	// Identity-less event: never invent a claim key.
	d := NewEventDedup(newRecordingStore(), "sesame:seen:", time.Minute, zap.NewNop())
	key, ttl = d.CounterClaim(&lane.Envelope{}, "deaths")
	assert.Empty(t, key)
	assert.Zero(t, ttl)
}

// --- dispatch: guardedCounterBump over CounterBumpOnce ---

// fakeBumpOnceLoyalty records the folded-bump call and serves canned replies. It
// implements LoyaltyStore; only the two methods guardedCounterBump reaches carry
// behavior.
type fakeBumpOnceLoyalty struct {
	lastDedupKey string
	lastTTL      time.Duration
	onceCalls    int
	onceValue    int64
	onceApplied  bool
	onceErr      error

	peek      loyaltyrpc.Counter
	peekFound bool
	peekCalls int
}

func (f *fakeBumpOnceLoyalty) CounterBumpOnce(_ context.Context, _ uint64, _ string, _ Viewer, _ string, _ int64, dedupKey string, ttl time.Duration) (int64, bool, error) {
	f.onceCalls++
	f.lastDedupKey = dedupKey
	f.lastTTL = ttl
	return f.onceValue, f.onceApplied, f.onceErr
}

func (f *fakeBumpOnceLoyalty) CounterPeek(context.Context, uint64, string, uint64, string) (loyaltyrpc.Counter, bool, error) {
	f.peekCalls++
	return f.peek, f.peekFound, nil
}

func (f *fakeBumpOnceLoyalty) Earn(uint64, uint64, string, string, int64, uint64) {}
func (f *fakeBumpOnceLoyalty) CounterBump(context.Context, uint64, string, Viewer, string, int64) (int64, error) {
	return 0, nil
}
func (f *fakeBumpOnceLoyalty) BalanceGet(context.Context, uint64, uint64) (loyaltyrpc.Balance, error) {
	return loyaltyrpc.Balance{}, nil
}
func (f *fakeBumpOnceLoyalty) BalanceAdjust(context.Context, uint64, string, int64, bool) (loyaltyrpc.Balance, bool, error) {
	return loyaltyrpc.Balance{}, false, nil
}
func (f *fakeBumpOnceLoyalty) CounterCreate(context.Context, uint64, string, string) (loyaltyrpc.Counter, error) {
	return loyaltyrpc.Counter{}, nil
}
func (f *fakeBumpOnceLoyalty) CounterSet(context.Context, uint64, string, uint64, string, int64) (bool, error) {
	return false, nil
}
func (f *fakeBumpOnceLoyalty) CounterDelete(context.Context, uint64, string) error { return nil }
func (f *fakeBumpOnceLoyalty) CounterList(context.Context, uint64) ([]loyaltyrpc.Counter, error) {
	return nil, nil
}

func bumpCtx(msgID string) *module.Context {
	return &module.Context{Env: chatEnv(msgID), BroadcasterID: 123, Log: zap.NewNop()}
}

func TestGuardedCounterBumpAppliedRendersValue(t *testing.T) {
	fake := &fakeBumpOnceLoyalty{onceValue: 43, onceApplied: true}
	dd := NewEventDedup(newRecordingStore(), "sesame:seen:", time.Minute, zap.NewNop())
	p := &Pipeline{loyalty: fake, dedup: dd, log: zap.NewNop()}

	value, ok := p.guardedCounterBump(context.Background(), bumpCtx("evt-1"), "deaths", Viewer{ID: 999}, "")
	require.True(t, ok)
	assert.Equal(t, "43", value)
	assert.Equal(t, 1, fake.onceCalls)
	assert.Zero(t, fake.peekCalls, "an applied bump renders inline, no peek")
	assert.Equal(t, "sesame:seen:evt-1:cbump:deaths", fake.lastDedupKey)
	assert.Equal(t, time.Minute, fake.lastTTL)
}

func TestGuardedCounterBumpKillSwitchWritesNoClaim(t *testing.T) {
	fake := &fakeBumpOnceLoyalty{onceValue: 7, onceApplied: true}
	// nil dedup = SESAME_IDEMPOTENCY off: bump once, no claim key.
	p := &Pipeline{loyalty: fake, dedup: nil, log: zap.NewNop()}

	value, ok := p.guardedCounterBump(context.Background(), bumpCtx("evt-1"), "deaths", Viewer{ID: 999}, "")
	require.True(t, ok)
	assert.Equal(t, "7", value)
	assert.Equal(t, 1, fake.onceCalls)
	assert.Empty(t, fake.lastDedupKey, "the kill switch passes no dedup key")
}

func TestGuardedCounterBumpDupColdFallsBackToPeek(t *testing.T) {
	fake := &fakeBumpOnceLoyalty{
		onceErr:   errCounterDupCold,
		peek:      loyaltyrpc.Counter{Name: "deaths", Value: 9},
		peekFound: true,
	}
	dd := NewEventDedup(newRecordingStore(), "sesame:seen:", time.Minute, zap.NewNop())
	p := &Pipeline{loyalty: fake, dedup: dd, log: zap.NewNop()}

	value, ok := p.guardedCounterBump(context.Background(), bumpCtx("evt-1"), "deaths", Viewer{ID: 999}, "")
	require.True(t, ok)
	assert.Equal(t, "9", value, "a dup-cold replay renders the authoritative peek")
	assert.Equal(t, 1, fake.peekCalls)
}

func TestGuardedCounterBumpFaultUnknownLeavesTokenUnbound(t *testing.T) {
	fake := &fakeBumpOnceLoyalty{onceErr: errors.New("valkey down"), peekFound: false}
	dd := NewEventDedup(newRecordingStore(), "sesame:seen:", time.Minute, zap.NewNop())
	p := &Pipeline{loyalty: fake, dedup: dd, log: zap.NewNop()}

	value, ok := p.guardedCounterBump(context.Background(), bumpCtx("evt-1"), "deaths", Viewer{ID: 999}, "")
	assert.False(t, ok, "an unresolvable value leaves the token visible")
	assert.Empty(t, value)
	assert.Equal(t, 1, fake.peekCalls)
}
