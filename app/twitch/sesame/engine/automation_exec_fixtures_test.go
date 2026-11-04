// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"ItsBagelBot/internal/domain/automation"
	"ItsBagelBot/internal/domain/outgress"
	automationrpc "ItsBagelBot/internal/domain/rpc/automation"
	automationsrpc "ItsBagelBot/internal/domain/rpc/automations"
	gossiprpc "ItsBagelBot/internal/domain/rpc/gossip"
	loyaltyrpc "ItsBagelBot/internal/domain/rpc/loyalty"
	outgressrpc "ItsBagelBot/internal/domain/rpc/outgress"
	"ItsBagelBot/pkg/codec"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type autoSource struct{ list []automation.Automation }

func (s autoSource) Automations(context.Context, uint64) ([]automation.Automation, error) {
	return s.list, nil
}

func (s autoSource) AutomationIndex(context.Context, uint64) (*automation.TriggerIndex, error) {
	return automation.CompileIndex(s.list), nil
}

type autoRunLog struct {
	mu   sync.Mutex
	recs []automationrpc.RunRecord
}

func (l *autoRunLog) Record(_ context.Context, _ uint64, rec automationrpc.RunRecord) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.recs = append(l.recs, rec)
}

func (l *autoRunLog) Recent(context.Context, uint64, string, int) ([]automationrpc.RunRecord, error) {
	return nil, nil
}

func (l *autoRunLog) records() []automationrpc.RunRecord {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]automationrpc.RunRecord(nil), l.recs...)
}

func (l *autoRunLog) forAutomation(id string) []automationrpc.RunRecord {
	var out []automationrpc.RunRecord
	for _, r := range l.records() {
		if r.AutomationID == id {
			out = append(out, r)
		}
	}
	return out
}

type autoLoyalty struct {
	LoyaltyStore
	mu      sync.Mutex
	adjusts []BalanceAdjustment
	bumps   []CounterBump
	sets    []string
}

func (l *autoLoyalty) BalanceAdjustViewer(_ context.Context, a BalanceAdjustment) (loyaltyrpc.Balance, bool, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.adjusts = append(l.adjusts, a)
	return loyaltyrpc.Balance{}, true, nil
}

func (l *autoLoyalty) CounterBump(_ context.Context, b CounterBump) (int64, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.bumps = append(l.bumps, b)
	return b.Delta, nil
}

func (l *autoLoyalty) CounterSet(_ context.Context, set CounterAssignment) (bool, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.sets = append(l.sets, fmt.Sprintf("%s/%d/%d", set.Name, set.ViewerID, set.Value))
	return true, nil
}

func (l *autoLoyalty) CounterPeek(context.Context, CounterTarget) (loyaltyrpc.Counter, bool, error) {
	return loyaltyrpc.Counter{}, false, nil
}

func (l *autoLoyalty) BalanceGet(context.Context, uint64, uint64) (loyaltyrpc.Balance, error) {
	return loyaltyrpc.Balance{}, nil
}

func (l *autoLoyalty) adjustCount() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.adjusts)
}

type autoGossip struct {
	mu     sync.Mutex
	route  GossipRoute
	req    gossiprpc.Request
	status gossiprpc.FetchStatus
	calls  int
	err    error

	outdated bool
}

func (g *autoGossip) Call(_ context.Context, route GossipRoute, req gossiprpc.Request, out any) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.route, g.req = route, req
	g.calls++
	if g.err != nil {
		return g.err
	}
	switch reply := out.(type) {
	case *gossiprpc.CustomFetchReply:
		reply.Status = g.status
	case *gossiprpc.GoveeControlReply:
		reply.SceneApplied = !g.outdated
	}
	return nil
}

type autoDiscord struct {
	mu    sync.Mutex
	posts []outgressrpc.DiscordPostRequest
	reply outgressrpc.DiscordPostReply
	err   error
}

func (d *autoDiscord) Post(_ context.Context, req outgressrpc.DiscordPostRequest) (outgressrpc.DiscordPostReply, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.posts = append(d.posts, req)
	return d.reply, d.err
}

func (d *autoDiscord) sent() []outgressrpc.DiscordPostRequest {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]outgressrpc.DiscordPostRequest(nil), d.posts...)
}

type autoAccounts map[string]string

func (a autoAccounts) ResolveLogin(_ context.Context, login string) (string, bool, error) {
	id, ok := a[login]
	return id, ok, nil
}

type autoFixture struct {
	p       *Pipeline
	pub     *fakePublisher
	runs    *autoRunLog
	loyalty *autoLoyalty
	gossip  *autoGossip
	discord *autoDiscord
}

func newAutoFixture(autos ...automation.Automation) *autoFixture {
	fx := &autoFixture{
		pub:     &fakePublisher{},
		runs:    &autoRunLog{},
		loyalty: &autoLoyalty{},
		gossip:  &autoGossip{status: gossiprpc.FetchOK},
		discord: &autoDiscord{reply: outgressrpc.DiscordPostReply{Checked: true}},
	}
	d := Deps{
		Proj: fakeReader{}, Live: liveAlways{}, Cooldown: NoopCooldown{}, Pub: fx.pub, Log: zap.NewNop(),
		Loyalty: fx.loyalty, Gossip: fx.gossip, Discord: fx.discord, TwitchAccounts: autoAccounts{"target": "99"},
		Dedup:       NewEventDedup(newRecordingStore(), "t:", time.Minute, zap.NewNop()),
		Automations: AutomationDeps{Source: autoSource{list: autos}, Runs: fx.runs},
	}
	fx.p = NewPipeline(d, NewRegistry(zap.NewNop()), Config{OutgressPremium: premiumSubj, OutgressStandard: standardSubj})
	return fx
}

func act(typ string, params any) automation.Action {
	raw, err := codec.Marshal(params)
	if err != nil {
		panic(err)
	}
	return automation.Action{Type: typ, Params: raw}
}

func disabledAction(a automation.Action) automation.Action {
	disabled := false
	a.Enabled = &disabled
	return a
}

func manual(id string, actions ...automation.Action) automation.Automation {
	return automation.Automation{
		ID: id, UserID: 1, Name: id, Enabled: true,
		Definition: automation.Definition{
			Version: 1, Actions: actions,
			Trigger: automation.Trigger{Type: "manual", Params: codec.RawMessage(`{}`)},
		},
	}
}

type obj = map[string]any

func (fx *autoFixture) run(t *testing.T, name, requestID string, args map[string]string) automationrpc.RunRecord {
	t.Helper()
	reply := fx.p.RunNamed(context.Background(), automationsrpc.RunRequest{
		BroadcasterID: "1", Name: name, Args: args, RequestID: requestID, Source: automationsrpc.SourceDashboard,
	})
	require.True(t, reply.Accepted, reply.Refusal)
	return fx.awaitRecord(t, reply.RunID)
}

func (fx *autoFixture) awaitRecord(t *testing.T, runID string) automationrpc.RunRecord {
	t.Helper()
	var found automationrpc.RunRecord
	require.Eventually(t, func() bool {
		for _, r := range fx.runs.records() {
			if r.RunID == runID {
				found = r
				return true
			}
		}
		return false
	}, 3*time.Second, 5*time.Millisecond)
	return found
}

func (fx *autoFixture) messages(t *testing.T) []outgress.Message {
	t.Helper()
	var out []outgress.Message
	for _, c := range fx.pub.snapshot() {
		out = append(out, flattenMessage(t, c.msg)...)
	}
	return out
}

func flattenMessage(t *testing.T, m outgress.Message) []outgress.Message {
	t.Helper()
	if m.Type != outgress.TypeBatch {
		return []outgress.Message{m}
	}
	var b outgress.Batch
	require.NoError(t, codec.Unmarshal(m.Payload, &b))
	return b.Items
}

func describe(m outgress.Message) string {
	parts := []string{m.Type}
	for _, kv := range [][2]string{
		{"to", m.To}, {"color", m.Color}, {"msg", m.MsgID}, {"reward", m.RewardID},
		{"redemption", m.RedemptionID}, {"status", m.Status},
	} {
		if kv[1] != "" {
			parts = append(parts, kv[0]+"="+kv[1])
		}
	}
	if len(m.Payload) > 0 && string(m.Payload) != "null" {
		parts = append(parts, string(m.Payload))
	}
	return strings.Join(parts, " ")
}

func (fx *autoFixture) described(t *testing.T) []string {
	t.Helper()
	var out []string
	for _, m := range fx.messages(t) {
		out = append(out, describe(m))
	}
	return out
}

func statuses(rec automationrpc.RunRecord) []string {
	out := make([]string, 0, len(rec.Actions))
	for _, a := range rec.Actions {
		out = append(out, fmt.Sprintf("%d:%s:%s:%s", a.Index, a.Type, a.Status, a.Detail))
	}
	return out
}
