// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"context"
	"math"
	"strings"
	"sync"
	"testing"
	"time"

	"ItsBagelBot/app/twitch/sesame/engine"
	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/internal/domain/event/lane"
	"ItsBagelBot/internal/domain/outgress"
	gossiprpc "ItsBagelBot/internal/domain/rpc/gossip"
	loyaltyrpc "ItsBagelBot/internal/domain/rpc/loyalty"
	"ItsBagelBot/internal/projection"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/codec"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type collector struct{ out []module.Output }

func (c *collector) emit(o *module.Output) { c.out = append(c.out, *o) }

func findCmd(t *testing.T, m module.Module, name string) module.Command {
	t.Helper()
	for _, c := range m.Commands {
		if c.Name == name {
			return c
		}
	}
	t.Fatalf("command %q not declared", name)
	return module.Command{}
}

func runChatErr(t *testing.T, m module.Module, c *module.Context, text string) ([]module.Output, error) {
	t.Helper()
	c.Env.Text = text
	name, args, _ := strings.Cut(strings.TrimPrefix(strings.TrimSpace(text), "!"), " ")
	bound, num, ok := engine.NewRegistry(zap.NewNop(), m).ResolveCommand(strings.ToLower(name))
	require.True(t, ok, "no command or alias %q", name)
	c.Num = num
	var col collector
	err := bound.Cmd.Run(t.Context(), c, strings.TrimSpace(args), col.emit)
	return col.out, err
}

func runChat(t *testing.T, m module.Module, c *module.Context, text string) []module.Output {
	t.Helper()
	out, err := runChatErr(t, m, c, text)
	require.NoError(t, err)
	return out
}

func texts(out []module.Output) []string {
	var got []string
	for _, o := range out {
		got = append(got, o.Text)
	}
	return got
}

type fakeLive struct {
	live bool
	err  error
}

func (f *fakeLive) IsLive(context.Context, uint64) (bool, error)           { return f.live, f.err }
func (f *fakeLive) SetLive(context.Context, uint64, int64) (bool, error)   { return true, nil }
func (f *fakeLive) ClearLive(context.Context, uint64, int64) (bool, error) { return true, nil }

type fakeGreet struct {
	first   bool
	greeted []string
	resetN  int
}

func (f *fakeGreet) FirstGreet(_ context.Context, _ uint64, id string) (bool, error) {
	f.greeted = append(f.greeted, id)
	return f.first, nil
}
func (f *fakeGreet) ResetGreets(context.Context, uint64) error { f.resetN++; return nil }

func chatCtx(chatterID, login string, badges ...string) *module.Context {
	env := lane.Envelope{
		Type:                 "channel.chat.message",
		BroadcasterUserID:    "100",
		BroadcasterUserLogin: "streamer",
		ChatterUserID:        chatterID,
		ChatterUserLogin:     login,
	}
	for _, b := range badges {
		if b != "" {
			env.Badges = append(env.Badges, lane.Badge{SetID: b})
		}
	}
	return &module.Context{Env: env, BroadcasterID: 100, Log: zap.NewNop()}
}

func withConfig(c *module.Context, config string) *module.Context {
	if config != "" {
		c.Config = []byte(config)
	}
	return c
}

type fakeProj struct {
	commands map[string]projection.Command
	modules  []projection.ModuleView
	user     projection.User
	userErr  error

	modulesErr error
	cmdErr     error
}

func (f *fakeProj) User(context.Context, uint64) (projection.User, error) {
	return f.user, f.userErr
}

func (f *fakeProj) Modules(context.Context, uint64) (map[string]projection.ModuleView, error) {
	if f.modulesErr != nil {
		return nil, f.modulesErr
	}
	return projection.ModuleMap(f.modules), nil
}

func (f *fakeProj) Module(ctx context.Context, id uint64, name string) (projection.ModuleView, bool, error) {
	views, err := f.Modules(ctx, id)
	if err != nil {
		return projection.ModuleView{}, false, err
	}
	view, ok := views[name]
	return view, ok, nil
}

func (f *fakeProj) Command(_ context.Context, _ uint64, name string) (projection.Command, bool, error) {
	if f.cmdErr != nil {
		return projection.Command{}, false, f.cmdErr
	}
	cmd, ok := f.commands[name]
	return cmd, ok, nil
}

type fakeCooldown struct {
	engine.NoopCooldown
	keys  []string
	ttls  []time.Duration
	allow []bool
	err   error
}

func (f *fakeCooldown) Allow(_ context.Context, key string, ttl time.Duration) (bool, error) {
	f.keys = append(f.keys, key)
	f.ttls = append(f.ttls, ttl)
	if f.err != nil {
		return false, f.err
	}
	if len(f.allow) == 0 {
		return true, nil
	}
	ok := f.allow[0]
	f.allow = f.allow[1:]
	return ok, nil
}

type fakeAccountAge struct {
	result engine.AccountAgeResult
	err    error
	got    struct{ targetID, targetLogin string }
}

func (f *fakeAccountAge) Lookup(_ context.Context, targetID, targetLogin string) (engine.AccountAgeResult, error) {
	f.got = struct{ targetID, targetLogin string }{targetID, targetLogin}
	return f.result, f.err
}

func (f *fakeAccountAge) ResolveLogin(ctx context.Context, login string) (string, bool, error) {
	r, err := f.Lookup(ctx, "", login)
	return r.TargetID, r.UserFound, err
}

type eventInput struct {
	event   string
	payload string
	cfg     string
}

func eventCtx(in eventInput) *module.Context {
	c := &module.Context{
		Env:           lane.Envelope{Type: in.event, Event: []byte(in.payload)},
		BroadcasterID: 2,
		Log:           zap.NewNop(),
	}
	return withConfig(c, in.cfg)
}

func runEvent(t *testing.T, m module.Module, c *module.Context) []module.Output {
	t.Helper()
	h := m.Events[c.Env.Type]
	require.NotNil(t, h, "module must handle %s", c.Env.Type)
	var col collector
	require.NoError(t, h(t.Context(), c, col.emit))
	return col.out
}

type fakeGossip struct {
	mu        sync.Mutex
	calls     []fakeGossipCall
	replies   map[string]any
	sequences map[string][]any
	err       error
	done      chan struct{}
}

type fakeGossipCall struct {
	provider, endpoint string
	req                gossiprpc.Request
}

func (f *fakeGossip) Call(_ context.Context, route engine.GossipRoute, req gossiprpc.Request, out any) error {
	f.mu.Lock()
	f.calls = append(f.calls, fakeGossipCall{route.Provider, route.Endpoint, req})
	if f.done != nil {
		close(f.done)
		f.done = nil
	}
	f.mu.Unlock()

	if f.err != nil {
		return f.err
	}
	key := route.Provider + "." + route.Endpoint
	f.mu.Lock()
	reply, ok := f.replies[key]
	if sequence := f.sequences[key]; len(sequence) > 0 {
		reply, ok = sequence[0], true
		f.sequences[key] = sequence[1:]
	}
	f.mu.Unlock()
	if !ok {
		return bus.RPCReplyError{Message: "no responder"}
	}
	b, err := codec.Marshal(reply)
	if err != nil {
		return err
	}
	return codec.Unmarshal(b, out)
}

func (f *fakeGossip) lastCall(t *testing.T) fakeGossipCall {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	require.NotEmpty(t, f.calls)
	return f.calls[len(f.calls)-1]
}

func assertRefund(t *testing.T, out []module.Output) {
	t.Helper()
	require.Len(t, out, 2)
	assert.Equal(t, outgress.TypeChat, out[0].Type)
	assert.Contains(t, out[0].Text, "refunded")
	assert.Equal(t, outgress.TypeRedemptionUpdate, out[1].Type)
	assert.Equal(t, outgress.RedemptionCanceled, out[1].Status)
}

type earnCall struct {
	broadcasterID, viewerID uint64
	login, name             string
	points                  int64
	watchSeconds            uint64
}

type bumpCall struct {
	broadcasterID uint64
	name          string
	viewerID      uint64
	command       string
	delta         int64
}

type adjustCall struct {
	viewerID uint64
	login    string
	value    int64
	absolute bool
}

type transferCall struct {
	fromID   uint64
	targetID uint64
	login    string
	amount   int64
}

type topViewer struct {
	id, login, name string
	points          int64
}

type fakeLoyalty struct {
	engine.LoyaltyStore
	earns       []earnCall
	bumps       []bumpCall
	adjusts     []adjustCall
	transfers   []transferCall
	wagers      []engine.PointWager
	transferBad bool
	topViewers  []topViewer
	balances    map[string]int64
	bumpVal     int64
	counters    map[string]loyaltyrpc.Counter
	deleted     []string
	setCalls    int
	setFound    bool
	getPoints   int64
	wagerLimit  bool
}

func (f *fakeLoyalty) Earn(e engine.PointEarning) {
	f.earns = append(f.earns, earnCall{e.BroadcasterID, e.ViewerID, e.Login, e.Name, e.Points, e.WatchSeconds})
}

func (f *fakeLoyalty) CounterBump(_ context.Context, b engine.CounterBump) (int64, error) {
	f.bumps = append(f.bumps, bumpCall{b.BroadcasterID, engine.NormalizeCounterName(b.Name), b.Viewer.ID, b.Command, b.Delta})
	f.bumpVal += b.Delta
	return f.bumpVal, nil
}

func (f *fakeLoyalty) CounterPeek(_ context.Context, target engine.CounterTarget) (loyaltyrpc.Counter, bool, error) {
	c, ok := f.counters[engine.NormalizeCounterName(target.Name)]
	return c, ok, nil
}

func (f *fakeLoyalty) BalanceGet(context.Context, uint64, uint64) (loyaltyrpc.Balance, error) {
	if f.getPoints != 0 {
		return loyaltyrpc.Balance{Points: f.getPoints}, nil
	}
	return loyaltyrpc.Balance{Points: 1234, WatchSeconds: 7200}, nil
}

func (f *fakeLoyalty) BalanceAdjustViewer(_ context.Context, a engine.BalanceAdjustment) (loyaltyrpc.Balance, bool, error) {
	f.adjusts = append(f.adjusts, adjustCall{viewerID: a.ViewerID, login: a.ViewerLogin, value: a.Value, absolute: a.Absolute})
	if a.ViewerLogin == "ghost" {
		return loyaltyrpc.Balance{}, false, nil
	}
	bal := f.standing(a.ViewerLogin)
	if a.Absolute {
		bal.Points = a.Value
	} else {
		bal.Points += a.Value
	}
	f.balances[a.ViewerLogin] = bal.Points
	return bal, true, nil
}

func (f *fakeLoyalty) BalanceTransfer(_ context.Context, t engine.PointTransfer) (loyaltyrpc.Balance, bool, bool, error) {
	f.transfers = append(f.transfers, transferCall{t.FromViewerID, t.TargetViewerID, t.TargetLogin, t.Amount})
	if t.TargetLogin == "ghost" {
		return loyaltyrpc.Balance{}, false, false, nil
	}
	bal := f.standing(t.TargetLogin)
	sender := f.standing("sender")
	if f.transferBad || t.Amount > sender.Points {
		return sender, true, false, nil
	}
	sender.Points -= t.Amount
	bal.Points += t.Amount
	f.balances["sender"] = sender.Points
	f.balances[t.TargetLogin] = bal.Points
	return sender, true, true, nil
}

func (f *fakeLoyalty) BalanceWager(_ context.Context, wager engine.PointWager) (engine.WagerOutcome, error) {
	f.wagers = append(f.wagers, wager)
	if wager.Login == "ghost" {
		return engine.WagerOutcome{}, nil
	}
	if f.wagerLimit {
		return engine.WagerOutcome{Balance: loyaltyrpc.Balance{Points: math.MaxInt64}, Found: true, LimitExceeded: true}, nil
	}
	bal := f.standing(wager.Login)
	out := engine.WagerOutcome{Balance: bal, Found: true}
	if bal.Points < wager.Amount {
		return out, nil
	}
	if wager.Won {
		bal.Points += wager.Amount
	} else {
		bal.Points -= wager.Amount
	}
	f.balances[wager.Login] = bal.Points
	out.Balance, out.Applied = bal, true
	return out, nil
}

func (f *fakeLoyalty) Top(_ context.Context, _ uint64, limit int) ([]loyaltyrpc.Balance, error) {
	rows := make([]loyaltyrpc.Balance, 0, len(f.topViewers))
	for _, v := range f.topViewers {
		if len(rows) >= limit {
			break
		}
		rows = append(rows, loyaltyrpc.Balance{ViewerID: v.id, ViewerName: v.name, ViewerLogin: v.login, Points: v.points})
	}
	return rows, nil
}

func (f *fakeLoyalty) standing(login string) loyaltyrpc.Balance {
	if f.balances == nil {
		f.balances = map[string]int64{}
	}
	points, seen := f.balances[login]
	if !seen {
		points = 1234
		f.balances[login] = points
	}
	return loyaltyrpc.Balance{ViewerID: "9", ViewerLogin: login, Points: points}
}

func (f *fakeLoyalty) CounterCreate(_ context.Context, _ uint64, name, scope string) (loyaltyrpc.Counter, error) {
	return loyaltyrpc.Counter{Name: engine.NormalizeCounterName(name), Scope: scope}, nil
}

func (f *fakeLoyalty) CounterSet(context.Context, uint64, string, uint64, string, int64) (bool, error) {
	f.setCalls++
	return f.setFound, nil
}

func (f *fakeLoyalty) CounterDelete(_ context.Context, _ uint64, name string) error {
	f.deleted = append(f.deleted, engine.NormalizeCounterName(name))
	return nil
}

func (f *fakeLoyalty) CounterList(context.Context, uint64) ([]loyaltyrpc.Counter, error) {
	out := make([]loyaltyrpc.Counter, 0, len(f.counters))
	for _, c := range f.counters {
		out = append(out, c)
	}
	return out, nil
}

type textWant struct {
	exact    string
	contains []string
	excludes []string
}

func assertText(t *testing.T, got string, want textWant) {
	t.Helper()
	if want.exact != "" {
		assert.Equal(t, want.exact, got)
	}
	for _, s := range want.contains {
		assert.Contains(t, got, s)
	}
	for _, s := range want.excludes {
		assert.NotContains(t, got, s)
	}
}
