package engine

import (
	"context"
	"sync"
	"testing"

	"ItsBagelBot/app/sesame/automod"
	"ItsBagelBot/internal/domain/outgress"
	"ItsBagelBot/pkg/bus"

	"github.com/bytedance/sonic"
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

func TestCohortFansOutReputationPerSender(t *testing.T) {
	rep := newFakeRep()
	d := Deps{Proj: fakeReader{}, Live: liveAlways{}, Cooldown: NoopCooldown{}, Pub: &fakePublisher{}, Log: zap.NewNop(), Reputation: rep}
	p := NewPipeline(d, NewRegistry(zap.NewNop()), Config{OutgressPremium: premiumSubj, OutgressStandard: standardSubj})

	body, err := sonic.Marshal(map[string]any{
		"type":                chatType,
		"lane":                "standard",
		"broadcaster_user_id": "123",
		"text":                "spam",
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
