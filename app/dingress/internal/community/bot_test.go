// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package community

import (
	"context"
	"encoding/json"
	"testing"

	"ItsBagelBot/app/dingress/internal/store"
	"ItsBagelBot/internal/discordapi"
	ddiscord "ItsBagelBot/internal/domain/discord"
)

type fakeREST struct {
	messages []string
	embeds   []string
	channels []string
	moved    []string
	deleted  []string
	roles    []string
	replies  []string
	commands int
	kick     int
	ban      int
	timeout  int
	purge    int
	listed   []discordapi.Snowflake
}

func (f *fakeREST) SendMessage(_ context.Context, _, content string, _ bool) error {
	f.messages = append(f.messages, content)
	return nil
}
func (f *fakeREST) SendEmbed(_ context.Context, post discordapi.EmbedPost) (discordapi.Message, error) {
	f.embeds = append(f.embeds, post.Embed.Title+"|"+post.Embed.Description)
	return discordapi.Message{ChannelID: post.ChannelID, ID: "msg-1"}, nil
}
func (f *fakeREST) SendPanel(_ context.Context, post discordapi.EmbedPost, _ []discordapi.Button) (discordapi.Message, error) {
	f.embeds = append(f.embeds, post.Embed.Title)
	return discordapi.Message{ChannelID: post.ChannelID, ID: "panel-1"}, nil
}
func (f *fakeREST) CreateChannel(_ context.Context, ch discordapi.GuildChannel) (discordapi.Snowflake, error) {
	id := "ch-" + ch.Spec.Name
	f.channels = append(f.channels, id)
	return discordapi.Snowflake{ID: id, Name: ch.Spec.Name, Type: ch.Spec.Type}, nil
}
func (f *fakeREST) DeleteChannel(_ context.Context, ch discordapi.Snowflake) error {
	f.deleted = append(f.deleted, ch.ID)
	return nil
}
func (f *fakeREST) AddMemberRole(_ context.Context, r discordapi.MemberRole) error {
	f.roles = append(f.roles, r.UserID+":"+r.RoleID)
	return nil
}
func (f *fakeREST) MoveMember(_ context.Context, move discordapi.VoiceMove) error {
	f.moved = append(f.moved, move.UserID+">"+move.ChannelID)
	return nil
}
func (f *fakeREST) ModifyChannel(context.Context, discordapi.ChannelPatch) error { return nil }
func (f *fakeREST) TimeoutMember(context.Context, discordapi.MemberTimeout) error {
	f.timeout++
	return nil
}
func (f *fakeREST) KickMember(context.Context, discordapi.GuildMember) error { f.kick++; return nil }
func (f *fakeREST) BanMember(context.Context, discordapi.GuildMember) error  { f.ban++; return nil }
func (f *fakeREST) BulkDeleteMessages(context.Context, discordapi.Purge) error {
	f.purge++
	return nil
}
func (f *fakeREST) ListMessages(context.Context, string, int) ([]discordapi.Snowflake, error) {
	return f.listed, nil
}
func (f *fakeREST) InteractionCallback(_ context.Context, cb discordapi.Callback) error {
	f.replies = append(f.replies, cb.Content)
	return nil
}
func (f *fakeREST) BulkOverwriteCommands(context.Context, string, []discordapi.AppCommand) error {
	f.commands++
	return nil
}

type staticModules struct{ cfg ddiscord.Config }

func (s staticModules) Config(context.Context, string) (ddiscord.Config, bool) {
	if s.cfg.GuildID == "" {
		return ddiscord.Config{}, false
	}
	return s.cfg, true
}

func testBot(cfg ddiscord.Config) (*Bot, *fakeREST, *store.Mem) {
	rest := &fakeREST{listed: []discordapi.Snowflake{{ID: "m1"}, {ID: "m2"}}}
	mem := store.NewMem()
	mem.PutGuild(cfg.GuildID, "42")
	return &Bot{REST: rest, Store: mem, Modules: staticModules{cfg: cfg}}, rest, mem
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestWelcomeAndAutorole(t *testing.T) {
	bot, rest, _ := testBot(ddiscord.Config{
		GuildID: "g1", WelcomeChannelID: "welcome", MemberRoleID: "member", WelcomeEnabled: "",
	})
	err := bot.Dispatch(context.Background(), "GUILD_MEMBER_ADD", mustJSON(t, map[string]any{
		"guild_id": "g1",
		"user":     map[string]any{"id": "u1", "username": "Ada"},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if len(rest.embeds) != 1 {
		t.Fatalf("welcome embeds = %d", len(rest.embeds))
	}
	if len(rest.roles) != 1 {
		t.Fatalf("autorole = %v", rest.roles)
	}
}

func TestGoodbyeOffByDefault(t *testing.T) {
	bot, rest, _ := testBot(ddiscord.Config{GuildID: "g1", WelcomeChannelID: "welcome"})
	_ = bot.Dispatch(context.Background(), "GUILD_MEMBER_REMOVE", mustJSON(t, map[string]any{
		"guild_id": "g1",
		"user":     map[string]any{"id": "u1", "username": "Ada"},
	}))
	if len(rest.messages) != 0 {
		t.Fatalf("goodbye posted while off: %v", rest.messages)
	}
}

func TestJoinToCreateVoice(t *testing.T) {
	bot, rest, _ := testBot(ddiscord.Config{GuildID: "g1", VoiceHubID: "hub", VoiceEnabled: ""})
	err := bot.Dispatch(context.Background(), "VOICE_STATE_UPDATE", mustJSON(t, map[string]any{
		"guild_id": "g1", "channel_id": "hub", "user_id": "u1",
		"member": map[string]any{"user": map[string]any{"id": "u1", "username": "Ada"}},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if len(rest.channels) != 1 {
		t.Fatalf("clone = %v", rest.channels)
	}
	if len(rest.moved) != 1 {
		t.Fatalf("move = %v", rest.moved)
	}
}

func TestTicketOpenAndClose(t *testing.T) {
	bot, rest, _ := testBot(ddiscord.Config{
		GuildID: "g1", TicketsEnabled: "", TicketCategoryID: "cat",
	})
	open := mustJSON(t, map[string]any{
		"id": "i1", "token": "tok", "guild_id": "g1", "channel_id": "support",
		"data":   map[string]any{"custom_id": customTicketOpen},
		"member": map[string]any{"user": map[string]any{"id": "u1", "username": "Ada"}, "permissions": "8"},
	})
	if err := bot.Dispatch(context.Background(), "INTERACTION_CREATE", open); err != nil {
		t.Fatal(err)
	}
	if len(rest.channels) != 1 {
		t.Fatalf("ticket channel = %v", rest.channels)
	}
	closeEv := mustJSON(t, map[string]any{
		"id": "i2", "token": "tok", "guild_id": "g1", "channel_id": rest.channels[0],
		"data":   map[string]any{"name": "ticket", "options": []any{map[string]any{"name": "close", "type": 1}}},
		"member": map[string]any{"user": map[string]any{"id": "u1", "username": "Ada"}, "permissions": "8"},
	})
	if err := bot.Dispatch(context.Background(), "INTERACTION_CREATE", closeEv); err != nil {
		t.Fatal(err)
	}
	if len(rest.deleted) != 1 {
		t.Fatalf("deleted = %v", rest.deleted)
	}
}

func TestDailyAndRank(t *testing.T) {
	bot, rest, _ := testBot(ddiscord.Config{GuildID: "g1", LevelsEnabled: ""})
	daily := mustJSON(t, map[string]any{
		"id": "i1", "token": "tok", "guild_id": "g1",
		"data":   map[string]any{"name": "daily"},
		"member": map[string]any{"user": map[string]any{"id": "u1"}},
	})
	if err := bot.Dispatch(context.Background(), "INTERACTION_CREATE", daily); err != nil {
		t.Fatal(err)
	}
	if len(rest.replies) != 1 {
		t.Fatalf("daily reply = %v", rest.replies)
	}
	if err := bot.Dispatch(context.Background(), "INTERACTION_CREATE", daily); err != nil {
		t.Fatal(err)
	}
	if rest.replies[1] != "Already claimed today." {
		t.Fatalf("second daily = %q", rest.replies[1])
	}
}

func TestModerationRequiresPerms(t *testing.T) {
	bot, rest, _ := testBot(ddiscord.Config{GuildID: "g1"})
	kick := mustJSON(t, map[string]any{
		"id": "i1", "token": "tok", "guild_id": "g1",
		"data": map[string]any{"name": "kick", "options": []any{
			map[string]any{"name": "user", "type": 6, "value": "u2"},
		}},
		"member": map[string]any{"user": map[string]any{"id": "u1"}, "permissions": "0"},
	})
	_ = bot.Dispatch(context.Background(), "INTERACTION_CREATE", kick)
	if rest.kick != 0 {
		t.Fatal("kick without perms")
	}
	kick2 := mustJSON(t, map[string]any{
		"id": "i2", "token": "tok", "guild_id": "g1",
		"data": map[string]any{"name": "kick", "options": []any{
			map[string]any{"name": "user", "type": 6, "value": "u2"},
		}},
		"member": map[string]any{"user": map[string]any{"id": "u1"}, "permissions": "8"},
	})
	_ = bot.Dispatch(context.Background(), "INTERACTION_CREATE", kick2)
	if rest.kick != 1 {
		t.Fatal("admin kick")
	}
}

func TestLevelUpOnChat(t *testing.T) {
	bot, rest, mem := testBot(ddiscord.Config{GuildID: "g1", LevelsEnabled: ""})
	mem.SeedXP("g1", "u1", 90)
	err := bot.Dispatch(context.Background(), "MESSAGE_CREATE", mustJSON(t, map[string]any{
		"id": "m1", "guild_id": "g1", "channel_id": "chat",
		"content": "hi",
		"author":  map[string]any{"id": "u1", "username": "Ada"},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if len(rest.messages) != 1 {
		t.Fatalf("level-up messages = %v", rest.messages)
	}
}

func TestEmptyCloneIsDeleted(t *testing.T) {
	bot, rest, _ := testBot(ddiscord.Config{GuildID: "g1", VoiceHubID: "hub", VoiceEnabled: ""})
	_ = bot.Dispatch(context.Background(), "VOICE_STATE_UPDATE", mustJSON(t, map[string]any{
		"guild_id": "g1", "channel_id": "hub", "user_id": "u1",
		"member": map[string]any{"user": map[string]any{"id": "u1", "username": "Ada"}},
	}))
	cloneID := rest.channels[0]
	_ = bot.Dispatch(context.Background(), "VOICE_STATE_UPDATE", mustJSON(t, map[string]any{
		"guild_id": "g1", "channel_id": cloneID, "user_id": "u1",
		"member": map[string]any{"user": map[string]any{"id": "u1", "username": "Ada"}},
	}))
	_ = bot.Dispatch(context.Background(), "VOICE_STATE_UPDATE", mustJSON(t, map[string]any{
		"guild_id": "g1", "channel_id": "", "user_id": "u1",
		"member": map[string]any{"user": map[string]any{"id": "u1", "username": "Ada"}},
	}))
	if len(rest.deleted) != 1 || rest.deleted[0] != cloneID {
		t.Fatalf("deleted = %v want %s", rest.deleted, cloneID)
	}
}

func TestJoinLogsWhenWelcomeOff(t *testing.T) {
	bot, rest, _ := testBot(ddiscord.Config{
		GuildID: "g1", WelcomeEnabled: "off", LogsEnabled: "", LogChannelID: "logs",
	})
	_ = bot.Dispatch(context.Background(), "GUILD_MEMBER_ADD", mustJSON(t, map[string]any{
		"guild_id": "g1",
		"user":     map[string]any{"id": "u1", "username": "Ada"},
	}))
	if len(rest.embeds) != 1 {
		t.Fatalf("log embeds = %v", rest.embeds)
	}
}

func TestReadyRegistersSlash(t *testing.T) {
	bot, rest, _ := testBot(ddiscord.Config{GuildID: "g1"})
	if err := bot.Ready(context.Background(), "app-1", "bot-1"); err != nil {
		t.Fatal(err)
	}
	if rest.commands != 1 {
		t.Fatalf("commands registered = %d", rest.commands)
	}
}

func TestUnboundGuildIsIgnored(t *testing.T) {
	bot, rest, _ := testBot(ddiscord.Config{GuildID: "g1", WelcomeChannelID: "welcome"})
	_ = bot.Dispatch(context.Background(), "GUILD_MEMBER_ADD", mustJSON(t, map[string]any{
		"guild_id": "other",
		"user":     map[string]any{"id": "u1", "username": "Ada"},
	}))
	if len(rest.embeds) != 0 {
		t.Fatal("unbound guild posted")
	}
}
