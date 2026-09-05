// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package discordstore

import (
	"context"
	"errors"
	"testing"

	ddiscord "ItsBagelBot/internal/domain/discord"
	"ItsBagelBot/internal/domain/rpc/discorddata"
	"ItsBagelBot/pkg/codec"
)

const testPrefix = "bagel.rpc.discord-data"

// fakeRequester is the scripted transport. handlers is keyed by the verb (the
// subject with the prefix stripped); a verb with no handler fails the way an
// unreachable discord-data does, which is exactly the case the fallback rules
// are about.
type fakeRequester struct {
	handlers map[string]func(request []byte) any
	calls    []string
	fail     map[string]error
}

func newFakeRequester() *fakeRequester {
	return &fakeRequester{handlers: map[string]func([]byte) any{}, fail: map[string]error{}}
}

func (f *fakeRequester) on(verb string, handle func(request []byte) any) {
	f.handlers[verb] = handle
}

// reply scripts a verb that ignores its request.
func (f *fakeRequester) reply(verb string, out any) {
	f.on(verb, func([]byte) any { return out })
}

func (f *fakeRequester) Request(_ context.Context, subject string, request, reply any) error {
	verb := subject[len(testPrefix)+1:]
	f.calls = append(f.calls, verb)
	if err, ok := f.fail[verb]; ok {
		return err
	}
	handle, ok := f.handlers[verb]
	if !ok {
		return errors.New("discord-data unreachable: " + verb)
	}
	body, err := codec.FastMarshal(request)
	if err != nil {
		return err
	}
	out, err := codec.FastMarshal(handle(body))
	if err != nil {
		return err
	}
	return codec.FastUnmarshal(out, reply)
}

func (f *fakeRequester) called(verb string) int {
	n := 0
	for _, c := range f.calls {
		if c == verb {
			n++
		}
	}
	return n
}

// newTestRPCStore wires the scripted transport to an in-memory local half.
func newTestRPCStore(t *testing.T) (Store, *fakeRequester, *Mem) {
	t.Helper()
	rpc := newFakeRequester()
	mem := NewMem()
	return newRPCStore(rpc, mem, testPrefix, nil), rpc, mem
}

func TestRPCStoreBroadcasterCachesTheBinding(t *testing.T) {
	store, rpc, mem := newTestRPCStore(t)
	rpc.reply(discorddata.VerbBindingGet, discorddata.BindingGetReply{BroadcasterID: 77, Found: true})

	got, ok := store.Broadcaster(context.Background(), Guild{ID: "g1"})
	if !ok || got.ID != "77" {
		t.Fatalf("Broadcaster = %+v, %v; want {77}, true", got, ok)
	}
	if cached, ok := mem.Broadcaster(context.Background(), Guild{ID: "g1"}); !ok || cached.ID != "77" {
		t.Fatalf("the answer was not cached locally: %+v, %v", cached, ok)
	}
}

func TestRPCStoreBroadcasterFallsBackToTheCache(t *testing.T) {
	store, rpc, mem := newTestRPCStore(t)
	_ = mem.BindGuild(context.Background(), Binding{Guild: Guild{ID: "g1"}, Broadcaster: Broadcaster{ID: "77"}})
	rpc.fail[discorddata.VerbBindingGet] = errors.New("nats: timeout")

	got, ok := store.Broadcaster(context.Background(), Guild{ID: "g1"})
	if !ok || got.ID != "77" {
		t.Fatalf("an unreachable discord-data must serve the cached binding, got %+v, %v", got, ok)
	}
}

func TestRPCStoreBroadcasterDropsTheCacheWhenTheBindingIsGone(t *testing.T) {
	store, rpc, mem := newTestRPCStore(t)
	_ = mem.BindGuild(context.Background(), Binding{Guild: Guild{ID: "g1"}, Broadcaster: Broadcaster{ID: "77"}})
	rpc.reply(discorddata.VerbBindingGet, discorddata.BindingGetReply{Found: false})

	if _, ok := store.Broadcaster(context.Background(), Guild{ID: "g1"}); ok {
		t.Fatal("an unbound guild must not answer from a stale cache entry")
	}
	if _, ok := mem.Broadcaster(context.Background(), Guild{ID: "g1"}); ok {
		t.Fatal("the stale cache entry survived")
	}
}

func TestRPCStoreBindGuildWritesThroughAndCaches(t *testing.T) {
	store, rpc, mem := newTestRPCStore(t)
	var seen discorddata.BindingSetRequest
	rpc.on(discorddata.VerbBindingSet, func(request []byte) any {
		_ = codec.FastUnmarshal(request, &seen)
		return discorddata.BindingSetReply{}
	})

	if err := store.BindGuild(context.Background(), Binding{Guild: Guild{ID: "g1"}, Broadcaster: Broadcaster{ID: "77"}}); err != nil {
		t.Fatalf("BindGuild: %v", err)
	}
	if seen.GuildID != "g1" || seen.BroadcasterID != 77 {
		t.Fatalf("request = %+v; want g1/77", seen)
	}
	if cached, ok := mem.Broadcaster(context.Background(), Guild{ID: "g1"}); !ok || cached.ID != "77" {
		t.Fatalf("the confirmed binding was not cached: %+v, %v", cached, ok)
	}
}

func TestRPCStoreBindGuildFailsLoudlyAndCachesNothing(t *testing.T) {
	store, rpc, mem := newTestRPCStore(t)
	rpc.fail[discorddata.VerbBindingSet] = errors.New("nats: no responders")

	if err := store.BindGuild(context.Background(), Binding{Guild: Guild{ID: "g1"}, Broadcaster: Broadcaster{ID: "77"}}); err == nil {
		t.Fatal("an unreachable discord-data must fail the write, not silently ack it")
	}
	if _, ok := mem.Broadcaster(context.Background(), Guild{ID: "g1"}); ok {
		t.Fatal("a failed write must not seed the cache")
	}
}

func TestRPCStoreBindGuildSurfacesBoundElsewhere(t *testing.T) {
	store, rpc, _ := newTestRPCStore(t)
	rpc.reply(discorddata.VerbBindingSet, discorddata.BindingSetReply{
		Error: "discord: already bound to a different broadcaster",
		Code:  discorddata.CodeBoundElsewhere,
	})

	err := store.BindGuild(context.Background(), Binding{Guild: Guild{ID: "g1"}, Broadcaster: Broadcaster{ID: "77"}})
	if !errors.Is(err, ErrBoundElsewhere) {
		t.Fatalf("BindGuild error = %v; want ErrBoundElsewhere", err)
	}
}

func TestRPCStoreBindGuildRejectsANonNumericBroadcaster(t *testing.T) {
	store, rpc, _ := newTestRPCStore(t)

	if err := store.BindGuild(context.Background(), Binding{Guild: Guild{ID: "g1"}, Broadcaster: Broadcaster{ID: "nope"}}); err == nil {
		t.Fatal("a non-numeric Twitch id must be refused before the round trip")
	}
	if len(rpc.calls) != 0 {
		t.Fatalf("no RPC should have been sent, got %v", rpc.calls)
	}
}

func TestRPCStoreUnbindDropsTheCacheOnlyOnSuccess(t *testing.T) {
	store, rpc, mem := newTestRPCStore(t)
	ctx := context.Background()
	_ = mem.BindGuild(ctx, Binding{Guild: Guild{ID: "g1"}, Broadcaster: Broadcaster{ID: "77"}})

	rpc.fail[discorddata.VerbBindingDelete] = errors.New("nats: timeout")
	unbind := Binding{Guild: Guild{ID: "g1"}, Broadcaster: Broadcaster{ID: "77"}}
	if err := store.UnbindGuild(ctx, unbind); err == nil {
		t.Fatal("a failed unbind must return an error")
	}
	if _, ok := mem.Broadcaster(ctx, Guild{ID: "g1"}); !ok {
		t.Fatal("a failed unbind must not drop the cached binding")
	}

	delete(rpc.fail, discorddata.VerbBindingDelete)
	rpc.reply(discorddata.VerbBindingDelete, discorddata.BindingDeleteReply{})
	if err := store.UnbindGuild(ctx, unbind); err != nil {
		t.Fatalf("UnbindGuild: %v", err)
	}
	if _, ok := mem.Broadcaster(ctx, Guild{ID: "g1"}); ok {
		t.Fatal("a confirmed unbind must drop the cached binding")
	}
}

// ticketRequests captures what the three ticket verbs actually put on the
// wire, so the round-trip test can assert on the requests without growing a
// closure per verb inline.
type ticketRequests struct {
	open  discorddata.TicketOpenRequest
	get   discorddata.TicketGetRequest
	close discorddata.TicketCloseRequest
}

// scriptTicketVerbs answers the three verbs and records their requests.
func scriptTicketVerbs(rpc *fakeRequester) *ticketRequests {
	sent := &ticketRequests{}
	rpc.on(discorddata.VerbTicketOpen, func(request []byte) any {
		_ = codec.FastUnmarshal(request, &sent.open)
		return discorddata.TicketOpenReply{TicketID: 1, OpenCount: 1}
	})
	rpc.on(discorddata.VerbTicketGet, func(request []byte) any {
		_ = codec.FastUnmarshal(request, &sent.get)
		return discorddata.TicketGetReply{
			Ticket: discorddata.Ticket{ChannelID: "c1", GuildID: "g1", OpenerID: "u1"},
			Found:  true,
		}
	})
	rpc.on(discorddata.VerbTicketClose, func(request []byte) any {
		_ = codec.FastUnmarshal(request, &sent.close)
		return discorddata.TicketCloseReply{TicketID: 1, OpenerID: "u1"}
	})
	return sent
}

func TestRPCStoreTicketRoundTrip(t *testing.T) {
	store, rpc, _ := newTestRPCStore(t)
	ctx := context.Background()
	sent := scriptTicketVerbs(rpc)

	if err := store.TrackTicket(ctx, Ticket{ChannelID: "c1", GuildID: "g1", OpenerID: "u1"}); err != nil {
		t.Fatalf("TrackTicket: %v", err)
	}
	got, ok := store.Ticket(ctx, Guild{ID: "g1"}, Channel{ID: "c1"})
	if !ok || got.OpenerID != "u1" || got.GuildID != "g1" {
		t.Fatalf("Ticket = %+v, %v", got, ok)
	}
	if err := store.ForgetTicket(ctx, Guild{ID: "g1"}, Channel{ID: "c1"}); err != nil {
		t.Fatalf("ForgetTicket: %v", err)
	}
	if sent.open.ChannelID != "c1" || sent.open.OpenerID != "u1" {
		t.Fatalf("open request = %+v", sent.open)
	}
}

// TestRPCStoreTicketVerbsCarryTheGuild: discord-data scopes the ticket lookup
// by guild and refuses a request that omits it, so a store that dropped the
// field would turn every button press into "this is not a ticket".
func TestRPCStoreTicketVerbsCarryTheGuild(t *testing.T) {
	store, rpc, _ := newTestRPCStore(t)
	ctx := context.Background()
	sent := scriptTicketVerbs(rpc)

	if _, ok := store.Ticket(ctx, Guild{ID: "g1"}, Channel{ID: "c1"}); !ok {
		t.Fatal("Ticket did not resolve")
	}
	if err := store.ForgetTicket(ctx, Guild{ID: "g1"}, Channel{ID: "c1"}); err != nil {
		t.Fatalf("ForgetTicket: %v", err)
	}
	if sent.get.GuildID != "g1" || sent.close.GuildID != "g1" {
		t.Fatalf("guild_id missing: get = %+v, close = %+v", sent.get, sent.close)
	}
}

func TestRPCStoreTicketReadsFalseWhenUnreachable(t *testing.T) {
	store, _, _ := newTestRPCStore(t)

	if _, ok := store.Ticket(context.Background(), Guild{ID: "g1"}, Channel{ID: "c1"}); ok {
		t.Fatal("an unreachable discord-data must read as 'not a ticket channel'")
	}
	if err := store.TrackTicket(context.Background(), Ticket{ChannelID: "c1", GuildID: "g1", OpenerID: "u1"}); err == nil {
		t.Fatal("an unreachable discord-data must fail the ticket write")
	}
}

func TestRPCStoreAddXPTakesTheLocalCooldownFirst(t *testing.T) {
	store, rpc, _ := newTestRPCStore(t)
	ctx := context.Background()
	member := Member{GuildID: "g1", UserID: "u1"}

	rpc.reply(discorddata.VerbXPAdd, discorddata.XPAddReply{XPValue: 100, Level: 1, LeveledUp: true})
	rpc.reply(discorddata.VerbXPGet, discorddata.XPGetReply{XPValue: 100, Level: 1, Found: true})

	xp, leveled, level := store.AddXP(ctx, member)
	if xp != 100 || !leveled || level != 1 {
		t.Fatalf("AddXP = %d, %v, %d; want 100, true, 1", xp, leveled, level)
	}

	// Second message inside the 60s window: no write, and the caller still gets
	// the current standing.
	xp, leveled, level = store.AddXP(ctx, member)
	if xp != 100 || leveled || level != 1 {
		t.Fatalf("cooled-down AddXP = %d, %v, %d; want 100, false, 1", xp, leveled, level)
	}
	if got := rpc.called(discorddata.VerbXPAdd); got != 1 {
		t.Fatalf("xp.add called %d times; the cooldown must keep it at 1", got)
	}
}

func TestRPCStoreDailyAndRank(t *testing.T) {
	store, rpc, _ := newTestRPCStore(t)
	ctx := context.Background()
	member := Member{GuildID: "g1", UserID: "u1"}

	rpc.reply(discorddata.VerbXPDaily, discorddata.XPDailyReply{Granted: true, XPValue: 50, Level: 0})
	ok, xp := store.ClaimDaily(ctx, member)
	if !ok || xp != 50 {
		t.Fatalf("ClaimDaily = %v, %d; want true, 50", ok, xp)
	}

	rpc.reply(discorddata.VerbXPDaily, discorddata.XPDailyReply{Granted: false, XPValue: 50})
	ok, xp = store.ClaimDaily(ctx, member)
	if ok || xp != 50 {
		t.Fatalf("second ClaimDaily = %v, %d; want false, 50", ok, xp)
	}

	rpc.reply(discorddata.VerbXPGet, discorddata.XPGetReply{XPValue: 400, Level: 2, Found: true})
	xp, level := store.Rank(ctx, member)
	if xp != 400 || level != 2 {
		t.Fatalf("Rank = %d, %d; want 400, 2", xp, level)
	}
}

func TestRPCStoreXPReadsZeroWhenUnreachable(t *testing.T) {
	store, _, _ := newTestRPCStore(t)
	member := Member{GuildID: "g1", UserID: "u1"}

	if xp, level := store.Rank(context.Background(), member); xp != 0 || level != 0 {
		t.Fatalf("Rank = %d, %d; want 0, 0", xp, level)
	}
	if ok, xp := store.ClaimDaily(context.Background(), member); ok || xp != 0 {
		t.Fatalf("ClaimDaily = %v, %d; want false, 0", ok, xp)
	}
}

// The three tests below are one composition guarantee split three ways: voice,
// clone and desk state must still be served by the embedded local store, with
// no RPC traffic at all. They were one function until it grew past the
// complexity gate; each now owns one keyspace, and noRPC carries the shared
// assertion.

// noRPC fails when any of the local keyspaces reached the wire.
func noRPC(t *testing.T, rpc *fakeRequester) {
	t.Helper()
	if len(rpc.calls) != 0 {
		t.Fatalf("the local keyspaces must cost no RPC, got %v", rpc.calls)
	}
}

func TestRPCStoreDelegatesTheCloneKeyspace(t *testing.T) {
	store, rpc, mem := newTestRPCStore(t)
	ctx := context.Background()

	if err := store.TrackClone(ctx, Clone{ChannelID: "v1", GuildID: "g1", OwnerID: "u1"}); err != nil {
		t.Fatalf("TrackClone: %v", err)
	}
	if got, ok := store.Clone(ctx, Channel{ID: "v1"}); !ok || got.OwnerID != "u1" {
		t.Fatalf("Clone = %+v, %v", got, ok)
	}
	if store.CloneCount(ctx, Guild{ID: "g1"}) != 1 {
		t.Fatal("CloneCount did not reach the local store")
	}
	if _, ok := mem.Clone(ctx, Channel{ID: "v1"}); !ok {
		t.Fatal("the clone did not land in the embedded local store")
	}
	noRPC(t, rpc)
}

func TestRPCStoreDelegatesTheDeskLock(t *testing.T) {
	store, rpc, _ := newTestRPCStore(t)
	ctx := context.Background()

	if !store.ClaimDesk(ctx, Guild{ID: "g1"}) || store.ClaimDesk(ctx, Guild{ID: "g1"}) {
		t.Fatal("the desk lock must be claimable exactly once")
	}
	noRPC(t, rpc)
}

func TestRPCStoreDelegatesVoiceOccupancy(t *testing.T) {
	store, rpc, _ := newTestRPCStore(t)
	ctx := context.Background()

	left, empty := store.UpdateVoiceOccupancy(ctx, VoiceSeat{GuildID: "g1", UserID: "u1", ChannelID: "v1"})
	if left != "" || empty {
		t.Fatalf("UpdateVoiceOccupancy = %q, %v; want the first seat to leave nothing", left, empty)
	}
	noRPC(t, rpc)
}

func TestRPCStoreGuildsOfListsEveryBoundGuild(t *testing.T) {
	store, rpc, _ := newTestRPCStore(t)
	rpc.reply(discorddata.VerbBindingListByBroadcaster, discorddata.BindingListByBroadcasterReply{
		Guilds: []discorddata.Binding{{GuildID: "g1", BoundAtUnixMs: 111}, {GuildID: "g2", BoundAtUnixMs: 222}},
	})

	got, err := store.GuildsOf(context.Background(), Broadcaster{ID: "42"})
	if err != nil {
		t.Fatalf("GuildsOf: %v", err)
	}
	if len(got) != 2 || got[0].Guild.ID != "g1" || got[1].Guild.ID != "g2" {
		t.Fatalf("want both guilds in order, got %v", got)
	}
	// The bind timestamp has to survive the hop: it is what the dashboard's
	// server card shows as "connected since".
	if got[0].BoundAtUnixMs != 111 || got[1].BoundAtUnixMs != 222 {
		t.Fatalf("bound_at_unix_ms lost in translation: %+v", got)
	}
}

// TestRPCStoreGuildsOfFailsLoudlyWhenTheStoreCannotSay is the property an
// empty slice used to hide: a caller cannot tell "this streamer connected no
// servers" from "the store is down" unless the store says so.
func TestRPCStoreGuildsOfFailsLoudlyWhenTheStoreCannotSay(t *testing.T) {
	store, _, _ := newTestRPCStore(t)

	got, err := store.GuildsOf(context.Background(), Broadcaster{ID: "42"})
	if !errors.Is(err, ErrStoreUnavailable) || got != nil {
		t.Fatalf("GuildsOf = %v, %v; want nil, ErrStoreUnavailable", got, err)
	}
	if _, err := store.GuildsOf(context.Background(), Broadcaster{ID: "not-numeric"}); err == nil {
		t.Fatal("a non-numeric broadcaster id must be refused before the wire")
	}
}

// TestRPCStoreGuildsOfCachesAndInvalidates pins the cache symmetry with
// Broadcaster: the listing is served from Valkey for guildsCacheTTL, and both
// write verbs drop it so a server added seconds ago shows up at once.
func TestRPCStoreGuildsOfCachesAndInvalidates(t *testing.T) {
	store, rpc, _ := newTestRPCStore(t)
	ctx := context.Background()
	rpc.reply(discorddata.VerbBindingListByBroadcaster, discorddata.BindingListByBroadcasterReply{
		Guilds: []discorddata.Binding{{GuildID: "g1"}},
	})
	rpc.reply(discorddata.VerbBindingSet, discorddata.BindingSetReply{})

	if _, err := store.GuildsOf(ctx, Broadcaster{ID: "42"}); err != nil {
		t.Fatalf("GuildsOf: %v", err)
	}
	if _, err := store.GuildsOf(ctx, Broadcaster{ID: "42"}); err != nil {
		t.Fatalf("GuildsOf: %v", err)
	}
	if got := rpc.called(discorddata.VerbBindingListByBroadcaster); got != 1 {
		t.Fatalf("binding.list_by_broadcaster called %d times; the cache must hold the second", got)
	}

	if err := store.BindGuild(ctx, Binding{Guild: Guild{ID: "g2"}, Broadcaster: Broadcaster{ID: "42"}}); err != nil {
		t.Fatalf("BindGuild: %v", err)
	}
	if _, err := store.GuildsOf(ctx, Broadcaster{ID: "42"}); err != nil {
		t.Fatalf("GuildsOf: %v", err)
	}
	if got := rpc.called(discorddata.VerbBindingListByBroadcaster); got != 2 {
		t.Fatalf("binding.list_by_broadcaster called %d times; a bind must invalidate the listing", got)
	}
}

func TestRPCStoreGuildConfigCachesAndServesTheCacheOnFailure(t *testing.T) {
	store, rpc, _ := newTestRPCStore(t)
	rpc.reply(discorddata.VerbConfigGet, discorddata.ConfigGetReply{
		Config: ddiscord.Config{LiveChannelID: "123"}, Version: 3, Found: true,
	})

	cfg, version, ok := store.GuildConfig(context.Background(), Guild{ID: "g1"})
	if !ok || version != 3 || cfg.LiveChannelID != "123" {
		t.Fatalf("want the stored settings, got %+v v%d ok=%v", cfg, version, ok)
	}

	// discord-data goes away: the cached settings keep the guild serving.
	rpc.fail[discorddata.VerbConfigGet] = errors.New("no responders")
	cfg, version, ok = store.GuildConfig(context.Background(), Guild{ID: "g1"})
	if !ok || version != 3 || cfg.LiveChannelID != "123" {
		t.Fatalf("want the cached settings, got %+v v%d ok=%v", cfg, version, ok)
	}
}

func TestRPCStoreGuildConfigDropsTheCacheWhenTheRowIsGone(t *testing.T) {
	store, rpc, mem := newTestRPCStore(t)
	mem.PutGuildConfig(Guild{ID: "g1"}, ddiscord.Config{LiveChannelID: "123"})
	rpc.reply(discorddata.VerbConfigGet, discorddata.ConfigGetReply{Found: false})

	if _, _, ok := store.GuildConfig(context.Background(), Guild{ID: "g1"}); ok {
		t.Fatal("a deleted settings row must not answer from cache")
	}
	if _, _, ok := mem.GuildConfig(context.Background(), Guild{ID: "g1"}); ok {
		t.Fatal("the stale cache entry must be dropped")
	}
}

func TestRPCStoreSetGuildConfigMapsTheReplyCodes(t *testing.T) {
	store, rpc, mem := newTestRPCStore(t)
	mem.PutGuildConfig(Guild{ID: "g1"}, ddiscord.Config{LiveChannelID: "123"})
	rpc.reply(discorddata.VerbConfigSet, discorddata.ConfigSetReply{Version: 4})

	set := SetConfig{Guild: Guild{ID: "g1"}, Broadcaster: Broadcaster{ID: "42"}, ExpectedVersion: 3}
	version, err := store.SetGuildConfig(context.Background(), set)
	if err != nil || version != 4 {
		t.Fatalf("want version 4, got %d err=%v", version, err)
	}
	if _, _, ok := mem.GuildConfig(context.Background(), Guild{ID: "g1"}); ok {
		t.Fatal("a successful write must invalidate the cache")
	}

	rpc.reply(discorddata.VerbConfigSet, discorddata.ConfigSetReply{
		Error: "stale", Code: discorddata.CodeConflict,
	})
	if _, err := store.SetGuildConfig(context.Background(), set); !errors.Is(err, ErrConfigConflict) {
		t.Fatalf("want ErrConfigConflict, got %v", err)
	}

	rpc.reply(discorddata.VerbConfigSet, discorddata.ConfigSetReply{
		Error: "not yours", Code: discorddata.CodeNotBound,
	})
	if _, err := store.SetGuildConfig(context.Background(), set); !errors.Is(err, ErrNotBound) {
		t.Fatalf("want ErrNotBound, got %v", err)
	}
}

func TestRPCStoreInvalidateDropsTheCachedSettings(t *testing.T) {
	store, _, mem := newTestRPCStore(t)
	mem.PutGuildConfig(Guild{ID: "g1"}, ddiscord.Config{LiveChannelID: "123"})

	store.Invalidate(context.Background(), Guild{ID: "g1"})

	if _, _, ok := mem.GuildConfig(context.Background(), Guild{ID: "g1"}); ok {
		t.Fatal("Invalidate must drop the entry")
	}
}

// TestRPCStoreBindGuildCarriesTheInstaller: discord-data records who ran the
// install so support can answer "who added this bot"; the field is useless if
// the store drops it on the way.
func TestRPCStoreBindGuildCarriesTheInstaller(t *testing.T) {
	store, rpc, _ := newTestRPCStore(t)
	var sent discorddata.BindingSetRequest
	rpc.on(discorddata.VerbBindingSet, func(request []byte) any {
		_ = codec.FastUnmarshal(request, &sent)
		return discorddata.BindingSetReply{}
	})

	err := store.BindGuild(context.Background(), Binding{
		Guild:       Guild{ID: "g1"},
		Broadcaster: Broadcaster{ID: "42"},
		InstalledBy: "discord-user-9",
	})
	if err != nil {
		t.Fatalf("BindGuild: %v", err)
	}
	if sent.InstalledBy != "discord-user-9" {
		t.Fatalf("installed_by = %q; want it threaded through", sent.InstalledBy)
	}
}

// TestRPCStoreUnbindCarriesTheBroadcaster: without it discord-data's owner
// guard cannot fire, and a stale unbind for a guild that has since been
// re-bound to somebody else drops the new owner's row.
func TestRPCStoreUnbindCarriesTheBroadcaster(t *testing.T) {
	store, rpc, _ := newTestRPCStore(t)
	var sent discorddata.BindingDeleteRequest
	rpc.on(discorddata.VerbBindingDelete, func(request []byte) any {
		_ = codec.FastUnmarshal(request, &sent)
		return discorddata.BindingDeleteReply{}
	})

	bind := Binding{Guild: Guild{ID: "g1"}, Broadcaster: Broadcaster{ID: "42"}}
	if err := store.UnbindGuild(context.Background(), bind); err != nil {
		t.Fatalf("UnbindGuild: %v", err)
	}
	if sent.BroadcasterID != 42 {
		t.Fatalf("broadcaster_id = %d; want the owner guard to have something to check", sent.BroadcasterID)
	}
}

// TestRPCStoreBindingOfReportsProvenance is what an ownership check reads. The
// event path may serve a cached binding through a discord-data blip; an
// ownership decision may not, so the two must be distinguishable.
func TestRPCStoreBindingOfReportsProvenance(t *testing.T) {
	store, rpc, _ := newTestRPCStore(t)
	ctx := context.Background()
	rpc.reply(discorddata.VerbBindingGet, discorddata.BindingGetReply{BroadcasterID: 42, Found: true})

	if _, source, ok := store.BindingOf(ctx, Guild{ID: "g1"}); !ok || source != BindingFromStore {
		t.Fatalf("BindingOf source = %v, ok = %v; want the authoritative answer", source, ok)
	}

	rpc.fail[discorddata.VerbBindingGet] = errors.New("nats: timeout")
	got, source, ok := store.BindingOf(ctx, Guild{ID: "g1"})
	if !ok || got.ID != "42" {
		t.Fatalf("BindingOf = %+v, %v; want the cached binding still served", got, ok)
	}
	if source != BindingFromCache {
		t.Fatalf("source = %v; a cache fallback must say so", source)
	}
}
