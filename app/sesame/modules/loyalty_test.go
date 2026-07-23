package modules

import (
	"context"
	"strings"
	"testing"

	"ItsBagelBot/app/sesame/engine"
	"ItsBagelBot/app/sesame/module"
	"ItsBagelBot/internal/domain/event/data"
	"ItsBagelBot/internal/domain/event/lane"
	loyaltyrpc "ItsBagelBot/internal/domain/rpc/loyalty"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

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
	login    string
	value    int64
	absolute bool
}

// fakeLoyalty records calls and serves canned counters.
type fakeLoyalty struct {
	earns    []earnCall
	bumps    []bumpCall
	adjusts  []adjustCall
	bumpVal  int64
	counters map[string]loyaltyrpc.Counter
	deleted  []string
	setCalls int
	setFound bool
}

func (f *fakeLoyalty) Earn(broadcasterID, viewerID uint64, login, name string, points int64, watchSeconds uint64) {
	f.earns = append(f.earns, earnCall{broadcasterID, viewerID, login, name, points, watchSeconds})
}

func (f *fakeLoyalty) CounterBump(_ context.Context, broadcasterID uint64, name string, viewer engine.Viewer, command string, delta int64) (int64, error) {
	f.bumps = append(f.bumps, bumpCall{broadcasterID, engine.NormalizeCounterName(name), viewer.ID, command, delta})
	f.bumpVal += delta
	return f.bumpVal, nil
}

func (f *fakeLoyalty) CounterPeek(_ context.Context, _ uint64, name string, _ uint64, _ string) (loyaltyrpc.Counter, bool, error) {
	c, ok := f.counters[engine.NormalizeCounterName(name)]
	return c, ok, nil
}

func (f *fakeLoyalty) BalanceGet(_ context.Context, _, viewerID uint64) (loyaltyrpc.Balance, error) {
	return loyaltyrpc.Balance{Points: 1234, WatchSeconds: 7200}, nil
}

func (f *fakeLoyalty) BalanceAdjust(_ context.Context, _ uint64, viewerLogin string, value int64, absolute bool) (loyaltyrpc.Balance, bool, error) {
	f.adjusts = append(f.adjusts, adjustCall{login: viewerLogin, value: value, absolute: absolute})
	if viewerLogin == "ghost" {
		return loyaltyrpc.Balance{}, false, nil
	}
	if absolute {
		return loyaltyrpc.Balance{ViewerID: "9", ViewerLogin: viewerLogin, Points: value}, true, nil
	}
	return loyaltyrpc.Balance{ViewerID: "9", ViewerLogin: viewerLogin, Points: 1234 + value}, true, nil
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

func loyaltyCtx(eventType, payload, config string) *module.Context {
	c := &module.Context{
		Env: lane.Envelope{
			Type:              eventType,
			Event:             []byte(payload),
			BroadcasterUserID: "2",
			ChatterUserID:     "7",
			ChatterUserLogin:  "coolviewer",
		},
		BroadcasterID: 2,
		Log:           zap.NewNop(),
	}
	if config != "" {
		c.Config = []byte(config)
	}
	return c
}

func loyaltyModule(t *testing.T, fake *fakeLoyalty) module.Module {
	t.Helper()
	m := Loyalty(engine.Deps{Loyalty: fake, Log: zap.NewNop()})
	assert.Equal(t, engine.LoyaltyModuleName, m.Name)
	assert.Equal(t, module.KindOptIn, m.Kind)
	return m
}

func loyaltyCommand(t *testing.T, m module.Module, name string) module.Command {
	t.Helper()
	for _, cmd := range m.Commands {
		if cmd.Name == name {
			return cmd
		}
	}
	t.Fatalf("loyalty must own the %q command", name)
	return module.Command{}
}

const loyaltySubJSON = `{"user_id":"7","user_login":"coolviewer","user_name":"CoolViewer","tier":"1000"}`

func TestLoyaltySubAwardsDefaultPoints(t *testing.T) {
	fake := &fakeLoyalty{}
	m := loyaltyModule(t, fake)
	var col collector
	require.NoError(t, m.Events["channel.subscribe"](context.Background(), loyaltyCtx("channel.subscribe", loyaltySubJSON, ""), col.emit))
	require.Len(t, fake.earns, 1)
	assert.Equal(t, earnCall{2, 7, "coolviewer", "CoolViewer", 500, 0}, fake.earns[0])
	assert.Empty(t, col.out, "accrual must be silent")
}

func TestLoyaltySubTierMultiplier(t *testing.T) {
	fake := &fakeLoyalty{}
	m := loyaltyModule(t, fake)
	tier3 := `{"user_id":"7","user_login":"coolviewer","user_name":"CoolViewer","tier":"3000"}`
	var col collector
	require.NoError(t, m.Events["channel.subscribe"](context.Background(), loyaltyCtx("channel.subscribe", tier3, ""), col.emit))
	require.Len(t, fake.earns, 1)
	assert.Equal(t, int64(3000), fake.earns[0].points)
}

func TestLoyaltySubSourceDisabled(t *testing.T) {
	fake := &fakeLoyalty{}
	m := loyaltyModule(t, fake)
	var col collector
	require.NoError(t, m.Events["channel.subscribe"](context.Background(), loyaltyCtx("channel.subscribe", loyaltySubJSON, `{"subPoints":-1}`), col.emit))
	assert.Empty(t, fake.earns)
}

func TestLoyaltyGiftCreditsGifter(t *testing.T) {
	fake := &fakeLoyalty{}
	m := loyaltyModule(t, fake)
	gift := `{"is_anonymous":false,"user_id":"7","user_login":"coolviewer","total":5,"tier":"1000"}`
	var col collector
	require.NoError(t, m.Events["channel.subscription.gift"](context.Background(), loyaltyCtx("channel.subscription.gift", gift, ""), col.emit))
	require.Len(t, fake.earns, 1)
	assert.Equal(t, int64(500), fake.earns[0].points) // 5 × default 100

	anon := `{"is_anonymous":true,"total":5,"tier":"1000"}`
	require.NoError(t, m.Events["channel.subscription.gift"](context.Background(), loyaltyCtx("channel.subscription.gift", anon, ""), col.emit))
	assert.Len(t, fake.earns, 1, "anonymous gifter earns nothing")
}

func TestLoyaltyCheerProRatesBits(t *testing.T) {
	fake := &fakeLoyalty{}
	m := loyaltyModule(t, fake)
	cheer := `{"is_anonymous":false,"user_id":"7","user_login":"coolviewer","bits":250}`
	var col collector
	require.NoError(t, m.Events["channel.cheer"](context.Background(), loyaltyCtx("channel.cheer", cheer, ""), col.emit))
	require.Len(t, fake.earns, 1)
	assert.Equal(t, int64(125), fake.earns[0].points) // 250 bits × 50/100
}

func TestLoyaltyPointsCommand(t *testing.T) {
	fake := &fakeLoyalty{}
	m := loyaltyModule(t, fake)
	cmd := loyaltyCommand(t, m, "points")
	assert.Equal(t, module.RoleEveryone, cmd.Perm)

	var col collector
	require.NoError(t, cmd.Run(context.Background(), loyaltyCtx("channel.chat.message", "", `{"pointsName":"bagels"}`), "", col.emit))
	require.Len(t, col.out, 1)
	assert.Contains(t, col.out[0].Text, "1234")
	assert.Contains(t, col.out[0].Text, "bagels")
	assert.Contains(t, col.out[0].Text, "2.0") // 7200s watched
}

func TestLoyaltyCounterCommand(t *testing.T) {
	fake := &fakeLoyalty{
		counters: map[string]loyaltyrpc.Counter{
			"deaths": {Name: "deaths", Scope: data.CounterScopeChannel, Value: 42},
		},
		setFound: true,
	}
	m := loyaltyModule(t, fake)
	cmd := loyaltyCommand(t, m, "counter")
	assert.Equal(t, module.RoleModerator, cmd.Perm)
	run := func(args string) string {
		var col collector
		require.NoError(t, cmd.Run(context.Background(), loyaltyCtx("channel.chat.message", "", ""), args, col.emit))
		require.Len(t, col.out, 1)
		return col.out[0].Text
	}

	assert.Contains(t, run(""), "Usage")
	assert.Contains(t, run("deaths"), "42")
	assert.Contains(t, run("add deaths 3"), "3")
	require.Len(t, fake.bumps, 1)
	assert.Equal(t, bumpCall{2, "deaths", 7, "", 3}, fake.bumps[0])
	assert.Contains(t, run("set deaths 10"), "10")
	assert.Equal(t, 1, fake.setCalls)
	assert.Contains(t, strings.ToLower(run("reset deaths")), "reset")
	assert.Contains(t, strings.ToLower(run("delete deaths")), "deleted")
	assert.Equal(t, []string{"deaths"}, fake.deleted)
	assert.Contains(t, run("list"), "deaths")
	assert.Contains(t, strings.ToLower(run("nosuch")), "not found")
	// The three creation modes, all per channel.
	assert.Contains(t, run("create wins"), "channel")
	assert.Contains(t, run("create wins user"), "per user")
	assert.Contains(t, run("create hugs user+command"), "per user+command")
}

func TestLoyaltyPointsModAdjust(t *testing.T) {
	fake := &fakeLoyalty{}
	m := loyaltyModule(t, fake)
	cmd := loyaltyCommand(t, m, "points")

	// The broadcaster (chatter id == broadcaster id) may set and add.
	modCtx := func() *module.Context {
		c := loyaltyCtx("channel.chat.message", "", "")
		c.Env.ChatterUserID = "2"
		return c
	}

	var col collector
	require.NoError(t, cmd.Run(context.Background(), modCtx(), "set @CoolViewer 500", col.emit))
	require.Len(t, fake.adjusts, 1)
	assert.Equal(t, adjustCall{login: "coolviewer", value: 500, absolute: true}, fake.adjusts[0])
	require.Len(t, col.out, 1)
	assert.Contains(t, col.out[0].Text, "500")
	assert.Contains(t, col.out[0].Text, "coolviewer")

	col = collector{}
	require.NoError(t, cmd.Run(context.Background(), modCtx(), "add coolviewer -100", col.emit))
	require.Len(t, fake.adjusts, 2)
	assert.Equal(t, adjustCall{login: "coolviewer", value: -100, absolute: false}, fake.adjusts[1])
	assert.Contains(t, col.out[0].Text, "1134")

	// Unknown target: no row to adjust, friendly reply.
	col = collector{}
	require.NoError(t, cmd.Run(context.Background(), modCtx(), "set ghost 10", col.emit))
	assert.Contains(t, strings.ToLower(col.out[0].Text), "haven't seen")

	// A plain viewer typing the verb gets their own balance, never a grant.
	col = collector{}
	require.NoError(t, cmd.Run(context.Background(), loyaltyCtx("channel.chat.message", "", ""), "set @CoolViewer 500", col.emit))
	assert.Len(t, fake.adjusts, 3, "non-mod must not reach the adjust path")
	assert.Contains(t, col.out[0].Text, "1234")
}

func TestChannelPointsPointsAward(t *testing.T) {
	fake := &fakeLoyalty{}
	m := ChannelPoints(engine.Deps{Loyalty: fake, Log: zap.NewNop()})
	h := m.Events[redemptionAddType]
	require.NotNil(t, h)

	cfg := `{"rewards":[{"id":"r1","action":"chat","message":"{user} bought {points} points!","points":250}]}`
	ev := `{"id":"red1","broadcaster_user_id":"2","user_id":"7","user_name":"CoolViewer","user_login":"coolviewer","reward":{"id":"r1","title":"Point Pack","cost":100}}`
	var col collector
	require.NoError(t, h(context.Background(), loyaltyCtx(redemptionAddType, ev, cfg), col.emit))

	require.Len(t, fake.earns, 1)
	assert.Equal(t, earnCall{2, 7, "coolviewer", "CoolViewer", 250, 0}, fake.earns[0])
	require.Len(t, col.out, 1)
	assert.Equal(t, "CoolViewer bought 250 points!", col.out[0].Text)
}

func TestChannelPointsLoyaltyLiveOnly(t *testing.T) {
	cfg := `{"rewards":[{"id":"r1","action":"chat","message":"{user} +1","counter":"deaths","points":50,"liveOnly":true}]}`
	ev := `{"id":"red1","broadcaster_user_id":"2","user_id":"7","user_name":"CoolViewer","user_login":"coolviewer","reward":{"id":"r1","title":"+1 death","cost":100}}`

	// Offline: the chat reply still fires, but no counter bump and no points.
	offline := &fakeLoyalty{}
	m := ChannelPoints(engine.Deps{Loyalty: offline, Live: &fakeLive{live: false}, Log: zap.NewNop()})
	var col collector
	require.NoError(t, m.Events[redemptionAddType](context.Background(), loyaltyCtx(redemptionAddType, ev, cfg), col.emit))
	assert.Empty(t, offline.bumps, "offline redeem must not bump the counter")
	assert.Empty(t, offline.earns, "offline redeem must not award points")
	require.Len(t, col.out, 1, "chat reply still runs offline")

	// Live: both loyalty writes happen.
	online := &fakeLoyalty{}
	m = ChannelPoints(engine.Deps{Loyalty: online, Live: &fakeLive{live: true}, Log: zap.NewNop()})
	col = collector{}
	require.NoError(t, m.Events[redemptionAddType](context.Background(), loyaltyCtx(redemptionAddType, ev, cfg), col.emit))
	assert.Len(t, online.bumps, 1, "live redeem bumps the counter")
	assert.Len(t, online.earns, 1, "live redeem awards points")
}

func TestChannelPointsCounterBinding(t *testing.T) {
	fake := &fakeLoyalty{}
	m := ChannelPoints(engine.Deps{Loyalty: fake, Log: zap.NewNop()})
	h := m.Events[redemptionAddType]
	require.NotNil(t, h)

	cfg := `{"rewards":[{"id":"r1","action":"chat","message":"{user} death #{counter}","counter":"deaths"}]}`
	ev := `{"id":"red1","broadcaster_user_id":"2","user_id":"7","user_name":"CoolViewer","user_login":"coolviewer","reward":{"id":"r1","title":"+1 death","cost":100}}`
	var col collector
	require.NoError(t, h(context.Background(), loyaltyCtx(redemptionAddType, ev, cfg), col.emit))

	require.Len(t, fake.bumps, 1)
	// The reward title rides as the bucket key, exactly like a command's
	// canonical name does on the command path.
	assert.Equal(t, bumpCall{2, "deaths", 7, "+1 death", 1}, fake.bumps[0])
	require.Len(t, col.out, 1)
	assert.Equal(t, "CoolViewer death #1", col.out[0].Text)
}
