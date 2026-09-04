// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"sync"
	"testing"

	"ItsBagelBot/app/twitch/sesame/automod"
	"ItsBagelBot/internal/domain/outgress"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/codec"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// fakeRep records Bumps and serves configurable Scores.
type fakeRep struct {
	mu     sync.Mutex
	bumps  map[string]int
	scores map[string]int
}

func newFakeRep() *fakeRep { return &fakeRep{bumps: map[string]int{}, scores: map[string]int{}} }

func (r *fakeRep) Bump(_ context.Context, id string) {
	r.mu.Lock()
	r.bumps[id]++
	r.mu.Unlock()
}

func (r *fakeRep) Score(_ context.Context, id string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.scores[id]
}

func TestEscalateByReputation(t *testing.T) {
	timeout := automod.Verdict{Action: automod.ActionTimeout, Seconds: 600, Rule: "scam"}
	assert.Equal(t, automod.ActionBan, escalateByReputation(timeout, repEscalateThreshold).Action)
	assert.Equal(t, automod.ActionTimeout, escalateByReputation(timeout, repEscalateThreshold-1).Action)

	// A non-timeout verdict is never escalated by reputation.
	del := automod.Verdict{Action: automod.ActionDelete, Rule: "heuristic"}
	assert.Equal(t, automod.ActionDelete, escalateByReputation(del, 99).Action)
}

// Strikes now require an enforceable hostile verdict, so the fan-out test
// arms enforcement and uses raid text: a benign or unenforced fold bumps nobody.
// Rationale for the changed expectation: strikes used to accrue before the
// verdict existed at all — even benign copypasta folds built repeat-offender
// records that later escalated punishments nobody had served.
func TestCohortFansOutReputationPerSender(t *testing.T) {
	rep := newFakeRep()
	d := Deps{
		Proj: fakeReader{}, Live: liveAlways{}, Cooldown: NoopCooldown{},
		Pub: &fakePublisher{}, Log: zap.NewNop(), Automod: automod.New(), Reputation: rep,
	}
	p := NewPipeline(d, NewRegistry(zap.NewNop()), Config{
		OutgressPremium: premiumSubj, OutgressStandard: standardSubj, AutomodEnforce: true,
	})

	body, err := codec.Marshal(map[string]any{
		"type":                chatType,
		"lane":                "standard",
		"broadcaster_user_id": "123",
		"text":                raidLink,
		"senders": []map[string]any{
			{"chatter_user_id": "a"},
			{"chatter_user_id": "b"},
			{"chatter_user_id": "a"},
		},
	})
	require.NoError(t, err)
	require.NoError(t, p.Process(bus.NewMessage("u", body)))

	assert.Equal(t, 2, rep.bumps["a"])
	assert.Equal(t, 1, rep.bumps["b"])
}

// A hostile fold only builds strikes when its punishment will actually be
// served: shadow mode and clean copypasta folds both leave scores untouched.
func TestCohortStrikesRequireEnforcedVerdict(t *testing.T) {
	tests := []struct {
		name      string
		enforce   bool
		text      string
		wantTotal int
	}{
		{name: "shadow hostile fold", enforce: false, text: raidLink, wantTotal: 0},
		{name: "benign fold under enforce", enforce: true, text: "PogChamp what a play", wantTotal: 0},
		{name: "hostile fold under enforce", enforce: true, text: raidLink, wantTotal: 3},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rep := newFakeRep()
			d := Deps{
				Proj: fakeReader{}, Live: liveAlways{}, Cooldown: NoopCooldown{},
				Pub: &fakePublisher{}, Log: zap.NewNop(), Automod: automod.New(), Reputation: rep,
			}
			p := NewPipeline(d, NewRegistry(zap.NewNop()), Config{
				OutgressPremium: premiumSubj, OutgressStandard: standardSubj, AutomodEnforce: tc.enforce,
			})
			require.NoError(t, p.Process(hostileCohort(t, 3, tc.text)))

			total := 0
			for _, n := range rep.bumps {
				total += n
			}
			assert.Equal(t, tc.wantTotal, total)
		})
	}
}

// Shadow-mode single-chatter verdicts are logged but must not score the
// chatter: arming enforcement day one would otherwise inherit escalated
// punishments from un-actioned history.
func TestShadowSingleChatterDoesNotBumpReputation(t *testing.T) {
	rep := newFakeRep()
	pub := &fakePublisher{}
	d := Deps{
		Proj: fakeReader{}, Live: liveAlways{}, Cooldown: NoopCooldown{},
		Pub: pub, Log: zap.NewNop(), Automod: automod.New(), Reputation: rep,
	}
	p := NewPipeline(d, NewRegistry(zap.NewNop()), Config{
		OutgressPremium: premiumSubj, OutgressStandard: standardSubj, AutomodEnforce: false,
	})

	require.NoError(t, p.Process(ipLoggerChat(t)))
	assert.Empty(t, pub.got, "shadow emits nothing")
	assert.Empty(t, rep.bumps, "and records no strike")
}

func TestReputationEscalatesTimeoutToBan(t *testing.T) {
	rep := newFakeRep()
	rep.scores["999"] = repEscalateThreshold + 2 // a repeat offender

	pub := &fakePublisher{}
	d := Deps{
		Proj: fakeReader{}, Live: liveAlways{}, Cooldown: NoopCooldown{},
		Pub: pub, Log: zap.NewNop(), Automod: automod.New(), Reputation: rep,
	}
	p := NewPipeline(d, NewRegistry(zap.NewNop()), Config{OutgressPremium: premiumSubj, OutgressStandard: standardSubj, AutomodEnforce: true})

	require.NoError(t, p.Process(ipLoggerChat(t)))
	require.Len(t, pub.got, 1)
	assert.Equal(t, outgress.TypeBan, pub.got[0].msg.Type, "a repeat offender's timeout escalates to a ban")
	assert.Equal(t, 1, rep.bumps["999"], "the offender's reputation is recorded")
}
