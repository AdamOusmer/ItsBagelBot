package modules

import (
	"context"
	"strconv"
	"strings"
	"testing"
	"time"

	"ItsBagelBot/app/sesame/engine"
	"ItsBagelBot/app/sesame/module"
	"ItsBagelBot/internal/domain/event/lane"
	"ItsBagelBot/internal/domain/outgress"
	gatewayrpc "ItsBagelBot/internal/domain/rpc/gateway"
	"ItsBagelBot/pkg/bus"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func fortniteCmd(t *testing.T, gw engine.GatewayCaller, name string) module.Command {
	t.Helper()
	m := Fortnite(engine.Deps{Gateway: gw, Log: zap.NewNop()})
	assert.Equal(t, "fortnite", m.Name)
	assert.Equal(t, module.KindOptIn, m.Kind)
	return findCmd(t, m, name)
}

func fortniteStatsReply() gatewayrpc.FortniteStatsReply {
	return gatewayrpc.FortniteStatsReply{
		Player:  "Ninja",
		Window:  "lifetime",
		Overall: gatewayrpc.FortniteModeStats{Wins: 301, Matches: 6232, Kills: 21679, KD: 3.66, WinRate: 4.83},
		Solo:    gatewayrpc.FortniteModeStats{Wins: 120, Matches: 2400, KD: 3.2},
		Duo:     gatewayrpc.FortniteModeStats{Wins: 90, Matches: 1900, KD: 3.8},
		Squad:   gatewayrpc.FortniteModeStats{Wins: 91, Matches: 1932, KD: 4.1},
	}
}

func TestFnstatsDefaultTemplate(t *testing.T) {
	gw := &fakeGateway{replies: map[string]any{"fortnite.stats": fortniteStatsReply()}}
	cmd := fortniteCmd(t, gw, "fnstats")

	var col collector
	require.NoError(t, cmd.Run(context.Background(), urchinCtx(""), "", col.emit))
	require.Len(t, col.out, 1)
	assert.Equal(t, outgress.TypeChat, col.out[0].Type)
	assert.Equal(t, "2", col.out[0].BroadcasterID)
	assert.Equal(t,
		"Ninja all time: 301 wins in 6232 matches · 4.83% WR · 21679 kills · 3.66 K/D · solo 120W / duo 90W / squad 91W",
		col.out[0].Text)

	// No linked account and no arg: falls back to the broadcaster's login. The
	// window is the command's, never configuration.
	call := gw.lastCall(t)
	assert.Equal(t, "streamer", call.req.Account)
	assert.Empty(t, call.req.AccountType)
	assert.Equal(t, "lifetime", call.req.TimeWindow)
}

func TestSeasonDefaultTemplate(t *testing.T) {
	reply := fortniteStatsReply()
	reply.Window = "season"
	gw := &fakeGateway{replies: map[string]any{"fortnite.stats": reply}}
	cmd := fortniteCmd(t, gw, "fnseason")

	var col collector
	require.NoError(t, cmd.Run(context.Background(), urchinCtx(""), "", col.emit))
	require.Len(t, col.out, 1)
	assert.Equal(t,
		"Ninja this season: 301 wins in 6232 matches · 4.83% WR · 21679 kills · 3.66 K/D · solo 120W / duo 90W / squad 91W",
		col.out[0].Text)
	assert.Equal(t, "season", gw.lastCall(t).req.TimeWindow)
}

func TestFnstatsConfigPassthrough(t *testing.T) {
	gw := &fakeGateway{replies: map[string]any{"fortnite.stats": fortniteStatsReply()}}
	cmd := fortniteCmd(t, gw, "fnstats")

	var col collector
	cfg := `{"account":"LinkedAcc","accountType":"psn"}`
	require.NoError(t, cmd.Run(context.Background(), urchinCtx(cfg), "", col.emit))

	call := gw.lastCall(t)
	assert.Equal(t, "LinkedAcc", call.req.Account)
	assert.Equal(t, "psn", call.req.AccountType)
	assert.Equal(t, "lifetime", call.req.TimeWindow)

	// An explicit argument still beats the linked account; the platform stays
	// the configured one.
	require.NoError(t, cmd.Run(context.Background(), urchinCtx(cfg), "@SomePlayer extra", col.emit))
	call = gw.lastCall(t)
	assert.Equal(t, "SomePlayer", call.req.Account)
	assert.Equal(t, "psn", call.req.AccountType)
}

// A per-command "off" toggle keeps that command silent: no chat line and no
// gateway call, for both fortnite commands.
func TestFortniteDisabledStaysSilent(t *testing.T) {
	cases := []struct{ name, config string }{
		{"fn", `{"statsEnabled":"off"}`},
		{"fnstats", `{"statsEnabled":"off"}`},
		{"fnseason", `{"seasonEnabled":"off"}`},
		{"fnsession", `{"sessionEnabled":"off"}`},
		{"fnstore", `{"storeEnabled":"off"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gw := &fakeGateway{}
			cmd := fortniteCmd(t, gw, tc.name)

			var col collector
			require.NoError(t, cmd.Run(context.Background(), urchinCtx(tc.config), "", col.emit))
			assert.Empty(t, col.out)
			gw.mu.Lock()
			assert.Empty(t, gw.calls)
			gw.mu.Unlock()
		})
	}
}

// The !fn root routes its first argument word: bare/player → all-time stats,
// season/store select the subcommand, and the remainder is the player arg.
func TestFnDispatch(t *testing.T) {
	seasonReply := fortniteStatsReply()
	seasonReply.Window = "season"

	cases := []struct {
		name, args      string
		endpoint        string // gateway endpoint expected
		window, account string // stats requests only
		replyValue      any
	}{
		{"bare is all-time stats", "", "stats", "lifetime", "streamer", fortniteStatsReply()},
		{"player arg is stats", "@SomePlayer extra", "stats", "lifetime", "SomePlayer", fortniteStatsReply()},
		{"season subcommand", "season", "stats", "season", "streamer", seasonReply},
		{"season with player", "season OtherGuy", "stats", "season", "OtherGuy", seasonReply},
		{"session subcommand", "session", "session", "", "", gatewayrpc.FortniteSessionReply{Player: "Ninja", HasSnapshot: true}},
		{"store subcommand", "store", "shop", "", "", gatewayrpc.FortniteShopReply{Date: "2026-07-10"}},
		{"shop alias, any case", "SHOP", "shop", "", "", gatewayrpc.FortniteShopReply{Date: "2026-07-10"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gw := &fakeGateway{replies: map[string]any{"fortnite." + tc.endpoint: tc.replyValue}}
			cmd := fortniteCmd(t, gw, "fn")

			var col collector
			require.NoError(t, cmd.Run(context.Background(), urchinCtx(""), tc.args, col.emit))
			require.Len(t, col.out, 1)

			call := gw.lastCall(t)
			assert.Equal(t, tc.endpoint, call.endpoint)
			if tc.endpoint == "stats" {
				assert.Equal(t, tc.window, call.req.TimeWindow)
				assert.Equal(t, tc.account, call.req.Account)
			}
		})
	}
}

func TestFnstatsReplyErrorChats(t *testing.T) {
	gw := &fakeGateway{err: bus.RPCReplyError{Message: "player not found"}}
	cmd := fortniteCmd(t, gw, "fnstats")

	var col collector
	require.NoError(t, cmd.Run(context.Background(), urchinCtx(""), "Ghosty", col.emit))
	require.Len(t, col.out, 1)
	assert.Equal(t, "Ghosty: player not found", col.out[0].Text)
}

// runFnSession runs !fnsession against a gateway stubbed with reply, under the
// given module config and command argument, returning the gateway and collected
// output for assertion.
func runFnSession(t *testing.T, reply gatewayrpc.FortniteSessionReply, cfg, arg string) (*fakeGateway, collector) {
	t.Helper()
	gw := &fakeGateway{replies: map[string]any{"fortnite.session": reply}}
	var col collector
	require.NoError(t, fortniteCmd(t, gw, "fnsession").Run(context.Background(), urchinCtx(cfg), arg, col.emit))
	return gw, col
}

func TestFnSessionDefaultTemplate(t *testing.T) {
	gw, col := runFnSession(t, gatewayrpc.FortniteSessionReply{
		Player: "Ninja", Wins: 3, Matches: 12, Kills: 48, KD: 5.33, WinRate: 25.0, HasSnapshot: true,
	}, `{"account":"Ninja"}`, "")
	require.Len(t, col.out, 1)
	assert.Equal(t, "Ninja this stream: 3 wins in 12 matches · 25% WR · 48 kills · 5.33 K/D", col.out[0].Text)

	// The session request is scoped to this channel and its linked account.
	call := gw.lastCall(t)
	assert.Equal(t, "session", call.endpoint)
	assert.Equal(t, "2", call.req.ChannelID)
	assert.Equal(t, "Ninja", call.req.Account)
}

// !fn session ignores a typed player argument so a viewer cannot retarget (and
// clobber) the streamer's per-channel baseline; it always uses the linked
// account.
func TestFnSessionIgnoresArgument(t *testing.T) {
	gw, _ := runFnSession(t,
		gatewayrpc.FortniteSessionReply{Player: "Ninja", HasSnapshot: true}, `{"account":"Ninja"}`, "SomeoneElse")
	assert.Equal(t, "Ninja", gw.lastCall(t).req.Account)
}

func TestFnSessionWithoutSnapshot(t *testing.T) {
	_, col := runFnSession(t, gatewayrpc.FortniteSessionReply{Player: "Ninja", HasSnapshot: false}, "", "")
	require.Len(t, col.out, 1)
	assert.Contains(t, col.out[0].Text, "session tracking just started")
}

// fortniteOnlineHandler builds the module and returns its stream.online handler.
func fortniteOnlineHandler(t *testing.T, gw engine.GatewayCaller) module.EventHandler {
	t.Helper()
	h := Fortnite(engine.Deps{Gateway: gw, Log: zap.NewNop()}).Events["stream.online"]
	require.NotNil(t, h, "fortnite must handle stream.online")
	return h
}

// fortniteOnlineCtx builds a stream.online Context for broadcaster 2 with cfg.
func fortniteOnlineCtx(cfg string) *module.Context {
	return &module.Context{
		Env:           lane.Envelope{Type: "stream.online", BroadcasterUserID: "2", BroadcasterUserLogin: "streamer"},
		BroadcasterID: 2,
		Log:           zap.NewNop(),
		Config:        []byte(cfg),
	}
}

// stream.online snapshots the linked account so !fn session has a baseline. The
// call is fire-and-forget on its own goroutine.
func TestFnStreamOnlineSnapshots(t *testing.T) {
	done := make(chan struct{})
	gw := &fakeGateway{
		replies: map[string]any{"fortnite.session_start": gatewayrpc.FortniteSnapshotReply{Player: "Ninja"}},
		done:    done,
	}
	h := fortniteOnlineHandler(t, gw)

	var col collector
	require.NoError(t, h(context.Background(), fortniteOnlineCtx(`{"account":"Ninja","accountType":"epic"}`), col.emit))
	assert.Empty(t, col.out, "snapshot handler must not chat")

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("stream.online never called the gateway")
	}
	call := gw.lastCall(t)
	assert.Equal(t, "fortnite", call.provider)
	assert.Equal(t, "session_start", call.endpoint)
	assert.Equal(t, "Ninja", call.req.Account)
	assert.Equal(t, "epic", call.req.AccountType)
	assert.Equal(t, "2", call.req.ChannelID)
}

// With the session command toggled off, stream.online must not spend the daily
// stats budget on a snapshot: the handler returns before spawning the call.
func TestFnStreamOnlineSkipsWhenSessionOff(t *testing.T) {
	gw := &fakeGateway{}
	h := fortniteOnlineHandler(t, gw)

	var col collector
	require.NoError(t, h(context.Background(), fortniteOnlineCtx(`{"account":"Ninja","sessionEnabled":"off"}`), col.emit))
	gw.mu.Lock()
	assert.Empty(t, gw.calls)
	gw.mu.Unlock()
}

func TestStoreDefaultTemplate(t *testing.T) {
	gw := &fakeGateway{replies: map[string]any{"fortnite.shop": gatewayrpc.FortniteShopReply{
		Date:  "2026-07-09",
		Count: 3,
		Entries: []gatewayrpc.FortniteShopEntry{
			{Name: "Peely Bundle", Price: 2800},
			{Name: "Renegade Raider", Price: 1200},
			{Name: "Free Hat"},
		},
	}}}
	cmd := fortniteCmd(t, gw, "fnstore")

	var col collector
	require.NoError(t, cmd.Run(context.Background(), urchinCtx(""), "", col.emit))
	require.Len(t, col.out, 1)
	assert.Equal(t,
		"Item Shop 2026-07-09: Peely Bundle (2800), Renegade Raider (1200), Free Hat",
		col.out[0].Text)
}

func TestFormatShopEntriesBudget(t *testing.T) {
	assert.Equal(t, "empty today", formatShopEntries(nil))

	// A long shop truncates within the budget and reports the remainder.
	var entries []gatewayrpc.FortniteShopEntry
	for i := 0; i < 60; i++ {
		entries = append(entries, gatewayrpc.FortniteShopEntry{
			Name: "Some Cosmetic Item " + strconv.Itoa(i), Price: 1200,
		})
	}
	got := formatShopEntries(entries)
	assert.LessOrEqual(t, len(got), fortniteShopBudget+len(" +99 more"))
	assert.Contains(t, got, " more")
	assert.True(t, strings.HasPrefix(got, "Some Cosmetic Item 0 (1200), "))

	// The first entry always renders, even when it alone blows the budget.
	huge := gatewayrpc.FortniteShopEntry{Name: strings.Repeat("x", fortniteShopBudget+50), Price: 100}
	got = formatShopEntries([]gatewayrpc.FortniteShopEntry{huge, {Name: "Next", Price: 1}})
	assert.True(t, strings.HasPrefix(got, huge.Name))
	assert.Contains(t, got, "+1 more")
}
