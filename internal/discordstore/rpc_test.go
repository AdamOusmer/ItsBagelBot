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
	_ = mem.BindGuild(context.Background(), Guild{ID: "g1"}, Broadcaster{ID: "77"})
	rpc.fail[discorddata.VerbBindingGet] = errors.New("nats: timeout")

	got, ok := store.Broadcaster(context.Background(), Guild{ID: "g1"})
	if !ok || got.ID != "77" {
		t.Fatalf("an unreachable discord-data must serve the cached binding, got %+v, %v", got, ok)
	}
}

func TestRPCStoreBroadcasterDropsTheCacheWhenTheBindingIsGone(t *testing.T) {
	store, rpc, mem := newTestRPCStore(t)
	_ = mem.BindGuild(context.Background(), Guild{ID: "g1"}, Broadcaster{ID: "77"})
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

	if err := store.BindGuild(context.Background(), Guild{ID: "g1"}, Broadcaster{ID: "77"}); err != nil {
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

	if err := store.BindGuild(context.Background(), Guild{ID: "g1"}, Broadcaster{ID: "77"}); err == nil {
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

	err := store.BindGuild(context.Background(), Guild{ID: "g1"}, Broadcaster{ID: "77"})
	if !errors.Is(err, ErrBoundElsewhere) {
		t.Fatalf("BindGuild error = %v; want ErrBoundElsewhere", err)
	}
}

func TestRPCStoreBindGuildRejectsANonNumericBroadcaster(t *testing.T) {
	store, rpc, _ := newTestRPCStore(t)

	if err := store.BindGuild(context.Background(), Guild{ID: "g1"}, Broadcaster{ID: "nope"}); err == nil {
		t.Fatal("a non-numeric Twitch id must be refused before the round trip")
	}
	if len(rpc.calls) != 0 {
		t.Fatalf("no RPC should have been sent, got %v", rpc.calls)
	}
}

func TestRPCStoreUnbindDropsTheCacheOnlyOnSuccess(t *testing.T) {
	store, rpc, mem := newTestRPCStore(t)
	ctx := context.Background()
	_ = mem.BindGuild(ctx, Guild{ID: "g1"}, Broadcaster{ID: "77"})

	rpc.fail[discorddata.VerbBindingDelete] = errors.New("nats: timeout")
	if err := store.UnbindGuild(ctx, Guild{ID: "g1"}); err == nil {
		t.Fatal("a failed unbind must return an error")
	}
	if _, ok := mem.Broadcaster(ctx, Guild{ID: "g1"}); !ok {
		t.Fatal("a failed unbind must not drop the cached binding")
	}

	delete(rpc.fail, discorddata.VerbBindingDelete)
	rpc.reply(discorddata.VerbBindingDelete, discorddata.BindingDeleteReply{})
	if err := store.UnbindGuild(ctx, Guild{ID: "g1"}); err != nil {
		t.Fatalf("UnbindGuild: %v", err)
	}
	if _, ok := mem.Broadcaster(ctx, Guild{ID: "g1"}); ok {
		t.Fatal("a confirmed unbind must drop the cached binding")
	}
}

func TestRPCStoreTicketRoundTrip(t *testing.T) {
	store, rpc, _ := newTestRPCStore(t)
	ctx := context.Background()

	var opened discorddata.TicketOpenRequest
	rpc.on(discorddata.VerbTicketOpen, func(request []byte) any {
		_ = codec.FastUnmarshal(request, &opened)
		return discorddata.TicketOpenReply{TicketID: 1, OpenCount: 1}
	})
	var fetched discorddata.TicketGetRequest
	rpc.on(discorddata.VerbTicketGet, func(request []byte) any {
		_ = codec.FastUnmarshal(request, &fetched)
		return discorddata.TicketGetReply{
			Ticket: discorddata.Ticket{ChannelID: "c1", GuildID: "g1", OpenerID: "u1"},
			Found:  true,
		}
	})
	var closed discorddata.TicketCloseRequest
	rpc.on(discorddata.VerbTicketClose, func(request []byte) any {
		_ = codec.FastUnmarshal(request, &closed)
		return discorddata.TicketCloseReply{TicketID: 1, OpenerID: "u1"}
	})

	if err := store.TrackTicket(ctx, Ticket{ChannelID: "c1", GuildID: "g1", OpenerID: "u1"}); err != nil {
		t.Fatalf("TrackTicket: %v", err)
	}
	if opened.ChannelID != "c1" || opened.OpenerID != "u1" {
		t.Fatalf("open request = %+v", opened)
	}

	got, ok := store.Ticket(ctx, Guild{ID: "g1"}, Channel{ID: "c1"})
	if !ok || got.OpenerID != "u1" || got.GuildID != "g1" {
		t.Fatalf("Ticket = %+v, %v", got, ok)
	}
	if err := store.ForgetTicket(ctx, Guild{ID: "g1"}, Channel{ID: "c1"}); err != nil {
		t.Fatalf("ForgetTicket: %v", err)
	}
	// The guild has to travel on both verbs: discord-data scopes the ticket
	// lookup by it, and refuses a request that omits it.
	if fetched.GuildID != "g1" || closed.GuildID != "g1" {
		t.Fatalf("guild_id missing: get = %+v, close = %+v", fetched, closed)
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

// TestRPCStoreDelegatesTheLocalKeyspaces is the composition guarantee: voice,
// clone and desk state must still be served by the embedded local store, with
// no RPC traffic at all.
func TestRPCStoreDelegatesTheLocalKeyspaces(t *testing.T) {
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
	if !store.ClaimDesk(ctx, Guild{ID: "g1"}) || store.ClaimDesk(ctx, Guild{ID: "g1"}) {
		t.Fatal("the desk lock must be claimable exactly once")
	}
	left, empty := store.UpdateVoiceOccupancy(ctx, VoiceSeat{GuildID: "g1", UserID: "u1", ChannelID: "v1"})
	if left != "" || empty {
		t.Fatalf("UpdateVoiceOccupancy = %q, %v; want the first seat to leave nothing", left, empty)
	}
	if _, ok := mem.Clone(ctx, Channel{ID: "v1"}); !ok {
		t.Fatal("the clone did not land in the embedded local store")
	}
	if len(rpc.calls) != 0 {
		t.Fatalf("the local keyspaces must cost no RPC, got %v", rpc.calls)
	}
}

func TestRPCStoreGuildsOfListsEveryBoundGuild(t *testing.T) {
	store, rpc, _ := newTestRPCStore(t)
	rpc.reply(discorddata.VerbBindingListByBroadcaster, discorddata.BindingListByBroadcasterReply{
		Guilds: []discorddata.Binding{{GuildID: "g1"}, {GuildID: "g2"}},
	})

	got := store.GuildsOf(context.Background(), Broadcaster{ID: "42"})
	if len(got) != 2 || got[0].ID != "g1" || got[1].ID != "g2" {
		t.Fatalf("want both guilds in order, got %v", got)
	}
}

func TestRPCStoreGuildsOfIsEmptyWhenTheStoreCannotSay(t *testing.T) {
	store, _, _ := newTestRPCStore(t)

	if got := store.GuildsOf(context.Background(), Broadcaster{ID: "42"}); got != nil {
		t.Fatalf("an unreachable discord-data must fan out to nothing, got %v", got)
	}
	if got := store.GuildsOf(context.Background(), Broadcaster{ID: "not-numeric"}); got != nil {
		t.Fatalf("a non-numeric broadcaster id must not reach the wire, got %v", got)
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
