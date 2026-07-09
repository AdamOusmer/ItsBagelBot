package modules

import (
	"context"
	"strconv"
	"strings"
	"testing"

	"ItsBagelBot/app/sesame/engine"
	"ItsBagelBot/app/sesame/module"
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
		"Ninja (lifetime): 301 wins in 6232 matches · 4.83% WR · 21679 kills · 3.66 K/D · solo 120W / duo 90W / squad 91W",
		col.out[0].Text)

	// No linked account and no arg: falls back to the broadcaster's login, and
	// blank platform/window ride to the gateway (which defaults them).
	call := gw.lastCall(t)
	assert.Equal(t, "streamer", call.req.Account)
	assert.Empty(t, call.req.AccountType)
	assert.Empty(t, call.req.TimeWindow)
}

func TestFnstatsConfigPassthrough(t *testing.T) {
	gw := &fakeGateway{replies: map[string]any{"fortnite.stats": fortniteStatsReply()}}
	cmd := fortniteCmd(t, gw, "fnstats")

	var col collector
	cfg := `{"account":"LinkedAcc","accountType":"psn","timeWindow":"season"}`
	require.NoError(t, cmd.Run(context.Background(), urchinCtx(cfg), "", col.emit))

	call := gw.lastCall(t)
	assert.Equal(t, "LinkedAcc", call.req.Account)
	assert.Equal(t, "psn", call.req.AccountType)
	assert.Equal(t, "season", call.req.TimeWindow)

	// An explicit argument still beats the linked account; platform and window
	// stay the configured ones.
	require.NoError(t, cmd.Run(context.Background(), urchinCtx(cfg), "@SomePlayer extra", col.emit))
	call = gw.lastCall(t)
	assert.Equal(t, "SomePlayer", call.req.Account)
	assert.Equal(t, "psn", call.req.AccountType)
}

func TestFnstatsDisabledStaysSilent(t *testing.T) {
	gw := &fakeGateway{replies: map[string]any{"fortnite.stats": fortniteStatsReply()}}
	cmd := fortniteCmd(t, gw, "fnstats")

	var col collector
	require.NoError(t, cmd.Run(context.Background(), urchinCtx(`{"statsEnabled":"off"}`), "", col.emit))
	assert.Empty(t, col.out)
	gw.mu.Lock()
	assert.Empty(t, gw.calls)
	gw.mu.Unlock()
}

func TestFnstatsReplyErrorChats(t *testing.T) {
	gw := &fakeGateway{err: bus.RPCReplyError{Message: "player not found"}}
	cmd := fortniteCmd(t, gw, "fnstats")

	var col collector
	require.NoError(t, cmd.Run(context.Background(), urchinCtx(""), "Ghosty", col.emit))
	require.Len(t, col.out, 1)
	assert.Equal(t, "Ghosty: player not found", col.out[0].Text)
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
	cmd := fortniteCmd(t, gw, "store")

	var col collector
	require.NoError(t, cmd.Run(context.Background(), urchinCtx(""), "", col.emit))
	require.Len(t, col.out, 1)
	assert.Equal(t,
		"Item Shop 2026-07-09: Peely Bundle (2800), Renegade Raider (1200), Free Hat",
		col.out[0].Text)
}

func TestStoreDisabledStaysSilent(t *testing.T) {
	gw := &fakeGateway{replies: map[string]any{"fortnite.shop": gatewayrpc.FortniteShopReply{}}}
	cmd := fortniteCmd(t, gw, "store")

	var col collector
	require.NoError(t, cmd.Run(context.Background(), urchinCtx(`{"storeEnabled":"off"}`), "", col.emit))
	assert.Empty(t, col.out)
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
