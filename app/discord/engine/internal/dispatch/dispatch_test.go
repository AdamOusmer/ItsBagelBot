// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package dispatch

import (
	"context"
	"sync"
	"testing"

	"ItsBagelBot/app/discord/engine/internal/registry"
	"ItsBagelBot/app/discord/engine/internal/resolve"
	"ItsBagelBot/app/discord/engine/modules"
	"ItsBagelBot/internal/discordapi"
	"ItsBagelBot/internal/discordstore"
	ddiscord "ItsBagelBot/internal/domain/discord"
	discordoutgress "ItsBagelBot/internal/domain/rpc/discordoutgress"
	"ItsBagelBot/internal/projection"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/codec"

	"go.uber.org/zap"
)

// This file replays app/dingress/internal/community/bot_test.go's scenarios
// against the split engine: a gateway payload goes in through Dispatcher.Handle
// exactly as it did through Bot.Dispatch, and instead of asserting on a fake
// REST client's call log, it asserts on the Commands Publish captured and the
// fake channel/purge RPC's call log -- the two things that replace "outgress
// called Discord" now that engine never does.

type fakeChannels struct {
	mu       sync.Mutex
	created  []string
	deleted  []string
	moved    []string
	modified []string
}

func (f *fakeChannels) CreateChannel(_ context.Context, req discordoutgress.ChannelCreateRequest) (discordoutgress.ChannelCreateReply, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	id := "ch-" + req.Name
	f.created = append(f.created, id)
	return discordoutgress.ChannelCreateReply{ChannelID: id}, nil
}

func (f *fakeChannels) DeleteChannel(_ context.Context, req discordoutgress.ChannelDeleteRequest) (discordoutgress.ChannelDeleteReply, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deleted = append(f.deleted, req.ChannelID)
	return discordoutgress.ChannelDeleteReply{}, nil
}

func (f *fakeChannels) ModifyChannel(_ context.Context, req discordoutgress.ChannelModifyRequest) (discordoutgress.ChannelModifyReply, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.modified = append(f.modified, req.ChannelID)
	return discordoutgress.ChannelModifyReply{}, nil
}

func (f *fakeChannels) MoveMember(_ context.Context, req discordoutgress.MemberMoveRequest) (discordoutgress.MemberMoveReply, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.moved = append(f.moved, req.UserID+">"+req.ChannelID)
	return discordoutgress.MemberMoveReply{}, nil
}

func (f *fakeChannels) Purge(context.Context, discordoutgress.PurgeRequest) (discordoutgress.PurgeReply, error) {
	return discordoutgress.PurgeReply{Deleted: 2}, nil
}

type fakeModules struct{ cfg ddiscord.Config }

func (m fakeModules) GetModule(context.Context, uint64, string) (projection.ModuleView, bool, error) {
	if m.cfg.GuildID == "" {
		return projection.ModuleView{}, false, nil
	}
	raw, _ := codec.Marshal(m.cfg)
	return projection.ModuleView{IsEnabled: true, Configs: raw}, true, nil
}

// commandLog captures every Command a test's Dispatcher publishes, keyed by
// Type for easy assertions ("did a PostEmbed happen").
type commandLog struct {
	mu   sync.Mutex
	cmds []ddiscord.Command
}

func (l *commandLog) publish(_ context.Context, c ddiscord.Command) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.cmds = append(l.cmds, c)
	return nil
}

func (l *commandLog) byType(t string) []ddiscord.Command {
	l.mu.Lock()
	defer l.mu.Unlock()
	var out []ddiscord.Command
	for _, c := range l.cmds {
		if c.Type == t {
			out = append(out, c)
		}
	}
	return out
}

func testDispatcher(cfg ddiscord.Config) (*Dispatcher, *fakeChannels, *discordstore.Mem, *commandLog) {
	channels := &fakeChannels{}
	store := discordstore.NewMem()
	store.PutGuild(discordstore.Guild{ID: cfg.GuildID}, discordstore.Broadcaster{ID: "42"})
	log := &commandLog{}

	// Tier reports premium: Discord is premium-only while it is in beta
	// (ddiscord.BetaPremiumOnly), and the resolver fails closed without a
	// reader. These tests are about dispatch, not the gate, so they run as a
	// premium channel; resolve's own tests cover the gate itself.
	resolver := resolve.Resolver{
		Store: store, Modules: fakeModules{cfg: cfg},
		Tier: func(context.Context, uint64) (string, bool) { return "paid", true },
		Log:  zap.NewNop(),
	}
	reg := registry.New(modules.All(modules.Deps{Store: store, Channels: channels, Purge: channels, Log: zap.NewNop()})...)
	d := &Dispatcher{Registry: reg, Resolver: resolver, Store: store, Publish: log.publish, Log: zap.NewNop()}
	return d, channels, store, log
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	raw, err := codec.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// event builds the ddiscord.Event a test's payload decodes as. Split out of
// dispatch below so dispatch itself takes one Event instead of its
// (eventType, guildID, payload) triple (CodeScene: Excess Number of
// Function Arguments).
func event(t *testing.T, eventType, guildID string, payload any) ddiscord.Event {
	t.Helper()
	return ddiscord.Event{Type: eventType, GuildID: guildID, Raw: mustJSON(t, payload)}
}

func dispatch(t *testing.T, d *Dispatcher, ev ddiscord.Event) {
	t.Helper()
	msg := bus.NewMessage("test", mustJSON(t, ev))
	if err := d.Handle(msg); err != nil {
		t.Fatalf("Handle: %v", err)
	}
}

func memberPayload(guildID string) map[string]any {
	return map[string]any{
		"guild_id": guildID,
		"user":     map[string]any{"id": "u1", "username": "Ada"},
	}
}

func TestWelcomeAndAutorole(t *testing.T) {
	d, _, _, log := testDispatcher(ddiscord.Config{GuildID: "g1", WelcomeChannelID: "welcome", MemberRoleID: "member"})
	dispatch(t, d, event(t, "GUILD_MEMBER_ADD", "g1", memberPayload("g1")))

	if got := log.byType(ddiscord.TypePostEmbed); len(got) != 1 {
		t.Fatalf("welcome embeds = %d", len(got))
	}
	if got := log.byType(ddiscord.TypeAddRole); len(got) != 1 {
		t.Fatalf("autorole = %d", len(got))
	}
}

func TestMemberDispatchGuards(t *testing.T) {
	cases := []struct {
		name    string
		cfg     ddiscord.Config
		event   string
		guildID string
		wantLog int
	}{
		{name: "goodbye off by default", cfg: ddiscord.Config{GuildID: "g1", WelcomeChannelID: "welcome"}, event: "GUILD_MEMBER_REMOVE", guildID: "g1", wantLog: 0},
		{name: "join logs when welcome off", cfg: ddiscord.Config{GuildID: "g1", WelcomeEnabled: "off", LogChannelID: "logs"}, event: "GUILD_MEMBER_ADD", guildID: "g1", wantLog: 1},
		{name: "unbound guild ignored", cfg: ddiscord.Config{GuildID: "g1", WelcomeChannelID: "welcome"}, event: "GUILD_MEMBER_ADD", guildID: "other", wantLog: 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d, _, _, log := testDispatcher(tc.cfg)
			dispatch(t, d, event(t, tc.event, tc.guildID, memberPayload(tc.guildID)))
			if got := log.byType(ddiscord.TypePostEmbed); len(got) != tc.wantLog {
				t.Fatalf("post-embed commands = %d, want %d", len(got), tc.wantLog)
			}
		})
	}
}

func voicePayload(guildID, channelID string) map[string]any {
	return map[string]any{
		"guild_id": guildID, "channel_id": channelID, "user_id": "u1",
		"member": map[string]any{"user": map[string]any{"id": "u1", "username": "Ada"}},
	}
}

func TestJoinToCreateVoice(t *testing.T) {
	d, channels, _, log := testDispatcher(ddiscord.Config{GuildID: "g1", VoiceHubID: "hub"})
	dispatch(t, d, event(t, "VOICE_STATE_UPDATE", "g1", voicePayload("g1", "hub")))

	if len(channels.created) != 1 {
		t.Fatalf("created = %v", channels.created)
	}
	if len(channels.moved) != 1 {
		t.Fatalf("moved = %v", channels.moved)
	}
	if got := log.byType(ddiscord.TypePostPanel); len(got) != 1 {
		t.Fatalf("voice room panel = %d", len(got))
	}
}

func TestEmptyCloneIsDeleted(t *testing.T) {
	d, channels, _, _ := testDispatcher(ddiscord.Config{GuildID: "g1", VoiceHubID: "hub"})
	dispatch(t, d, event(t, "VOICE_STATE_UPDATE", "g1", voicePayload("g1", "hub")))
	cloneID := channels.created[0]
	dispatch(t, d, event(t, "VOICE_STATE_UPDATE", "g1", voicePayload("g1", cloneID)))
	dispatch(t, d, event(t, "VOICE_STATE_UPDATE", "g1", voicePayload("g1", "")))

	if len(channels.deleted) != 1 || channels.deleted[0] != cloneID {
		t.Fatalf("deleted = %v, want [%s]", channels.deleted, cloneID)
	}
}

func interactionPayload(guildID, channelID string, data map[string]any, member map[string]any) map[string]any {
	return map[string]any{
		"id": "i1", "token": "tok", "guild_id": guildID, "channel_id": channelID,
		"data": data, "member": member,
	}
}

func TestTicketOpenAndClose(t *testing.T) {
	d, channels, _, log := testDispatcher(ddiscord.Config{GuildID: "g1", TicketCategoryID: "cat"})
	member := map[string]any{"user": map[string]any{"id": "u1", "username": "Ada"}, "permissions": "8"}

	dispatch(t, d, event(t, "INTERACTION_CREATE", "g1", interactionPayload("g1", "support",
		map[string]any{"custom_id": discordapi.CustomTicketOpen}, member)))
	if len(channels.created) != 1 {
		t.Fatalf("ticket channel = %v", channels.created)
	}
	if len(log.byType(ddiscord.TypePostPanel)) != 1 {
		t.Fatal("expected a panel posted into the new ticket channel")
	}

	dispatch(t, d, event(t, "INTERACTION_CREATE", "g1", interactionPayload("g1", channels.created[0],
		map[string]any{"custom_id": discordapi.CustomTicketClose}, member)))
	if len(channels.deleted) != 1 {
		t.Fatalf("deleted = %v", channels.deleted)
	}
}

func TestDailyAndRank(t *testing.T) {
	d, _, _, log := testDispatcher(ddiscord.Config{GuildID: "g1"})
	member := map[string]any{"user": map[string]any{"id": "u1"}}

	dispatch(t, d, event(t, "INTERACTION_CREATE", "g1", interactionPayload("g1", "",
		map[string]any{"name": "daily"}, member)))
	first := log.byType(ddiscord.TypeInteractionFollowup)
	if len(first) != 1 {
		t.Fatalf("daily reply = %d", len(first))
	}

	dispatch(t, d, event(t, "INTERACTION_CREATE", "g1", interactionPayload("g1", "",
		map[string]any{"name": "daily"}, member)))
	second := log.byType(ddiscord.TypeInteractionFollowup)
	if len(second) != 2 {
		t.Fatalf("second daily reply missing: %d", len(second))
	}
	var payload ddiscord.FollowupPayload
	if err := codec.Unmarshal(second[1].Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Embed == nil || payload.Embed.Description != "Already claimed today." {
		t.Fatalf("second daily embed = %+v", payload.Embed)
	}
}

func TestModerationRequiresPerms(t *testing.T) {
	d, _, _, log := testDispatcher(ddiscord.Config{GuildID: "g1"})
	kickData := map[string]any{"name": "kick", "options": []any{
		map[string]any{"name": "user", "type": 6, "value": "u2"},
	}}

	dispatch(t, d, event(t, "INTERACTION_CREATE", "g1", interactionPayload("g1", "",
		kickData, map[string]any{"user": map[string]any{"id": "u1"}, "permissions": "0"})))
	if len(log.byType(ddiscord.TypeKickMember)) != 0 {
		t.Fatal("kick without perms must not fire")
	}

	dispatch(t, d, event(t, "INTERACTION_CREATE", "g1", interactionPayload("g1", "",
		kickData, map[string]any{"user": map[string]any{"id": "u1"}, "permissions": "8"})))
	if len(log.byType(ddiscord.TypeKickMember)) != 1 {
		t.Fatal("admin kick must fire")
	}
}

func TestLevelUpOnChat(t *testing.T) {
	d, _, store, log := testDispatcher(ddiscord.Config{GuildID: "g1"})
	store.SeedXP(discordstore.XPSeed{Member: discordstore.Member{GuildID: "g1", UserID: "u1"}, Amount: 90})

	dispatch(t, d, event(t, "MESSAGE_CREATE", "g1", map[string]any{
		"id": "m1", "guild_id": "g1", "channel_id": "chat", "content": "hi",
		"author": map[string]any{"id": "u1", "username": "Ada"},
	}))
	if got := log.byType(ddiscord.TypePostEmbed); len(got) != 1 {
		t.Fatalf("level-up embeds = %d", len(got))
	}
}

func TestTicketDeskPostedOnce(t *testing.T) {
	d, _, _, log := testDispatcher(ddiscord.Config{GuildID: "g1", TicketChannelID: "support", WelcomeEnabled: "off"})
	dispatch(t, d, event(t, "GUILD_MEMBER_ADD", "g1", memberPayload("g1")))
	dispatch(t, d, event(t, "GUILD_MEMBER_ADD", "g1", memberPayload("g1")))

	panels := log.byType(ddiscord.TypePostPanel)
	if len(panels) != 1 {
		t.Fatalf("desk posts = %d, want 1", len(panels))
	}
}

func TestVoiceLockButton(t *testing.T) {
	d, channels, _, log := testDispatcher(ddiscord.Config{GuildID: "g1", VoiceHubID: "hub"})
	dispatch(t, d, event(t, "VOICE_STATE_UPDATE", "g1", voicePayload("g1", "hub")))

	dispatch(t, d, event(t, "INTERACTION_CREATE", "g1", interactionPayload("g1", channels.created[0],
		map[string]any{"custom_id": discordapi.CustomVoiceLock},
		map[string]any{"user": map[string]any{"id": "u1", "username": "Ada"}, "permissions": "0"})))

	followups := log.byType(ddiscord.TypeInteractionFollowup)
	if len(followups) != 1 {
		t.Fatalf("lock reply = %d", len(followups))
	}
	var payload ddiscord.FollowupPayload
	if err := codec.Unmarshal(followups[0].Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Content != "Locked." {
		t.Fatalf("lock reply content = %q", payload.Content)
	}
}
