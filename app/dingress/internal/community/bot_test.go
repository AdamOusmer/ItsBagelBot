// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package community

import (
	"context"
	"testing"

	"ItsBagelBot/app/dingress/internal/gateway"
	"ItsBagelBot/app/dingress/internal/store"
	"ItsBagelBot/internal/discordapi"
	ddiscord "ItsBagelBot/internal/domain/discord"
	"ItsBagelBot/pkg/codec"
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

func (f *fakeREST) SendChat(_ context.Context, post discordapi.ChatPost) error {
	f.messages = append(f.messages, post.Content)
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
func (f *fakeREST) ListMessages(_ context.Context, _ discordapi.MessageQuery) ([]discordapi.Snowflake, error) {
	return f.listed, nil
}
func (f *fakeREST) InteractionCallback(_ context.Context, cb discordapi.Callback) error {
	f.replies = append(f.replies, cb.Content)
	return nil
}
func (f *fakeREST) BulkOverwriteCommands(context.Context, discordapi.CommandCatalog) error {
	f.commands++
	return nil
}

type staticModules struct{ cfg ddiscord.Config }

func (s staticModules) Config(context.Context, store.Broadcaster) (ddiscord.Config, bool) {
	if s.cfg.GuildID == "" {
		return ddiscord.Config{}, false
	}
	return s.cfg, true
}

func testBot(cfg ddiscord.Config) (*Bot, *fakeREST, *store.Mem) {
	rest := &fakeREST{listed: []discordapi.Snowflake{{ID: "m1"}, {ID: "m2"}}}
	mem := store.NewMem()
	mem.PutGuild(store.Guild{ID: cfg.GuildID}, store.Broadcaster{ID: "42"})
	return &Bot{REST: rest, Store: mem, Modules: staticModules{cfg: cfg}}, rest, mem
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	raw, err := codec.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func dispatch(t *testing.T, bot *Bot, typ string, payload any) error {
	t.Helper()
	return bot.Dispatch(context.Background(), gateway.Event{Type: typ, Raw: mustJSON(t, payload)})
}

func memberPayload(guildID string) map[string]any {
	return map[string]any{
		"guild_id": guildID,
		"user":     map[string]any{"id": "u1", "username": "Ada"},
	}
}

func TestWelcomeAndAutorole(t *testing.T) {
	bot, rest, _ := testBot(ddiscord.Config{
		GuildID: "g1", WelcomeChannelID: "welcome", MemberRoleID: "member", WelcomeEnabled: "",
	})
	err := dispatch(t, bot, "GUILD_MEMBER_ADD", memberPayload("g1"))
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

func TestMemberDispatchGuards(t *testing.T) {
	cases := []struct {
		name    string
		cfg     ddiscord.Config
		event   string
		guildID string
		wantMsg int
		wantEmb int
	}{
		{
			name:    "goodbye off by default",
			cfg:     ddiscord.Config{GuildID: "g1", WelcomeChannelID: "welcome"},
			event:   "GUILD_MEMBER_REMOVE",
			guildID: "g1",
		},
		{
			name:    "join logs when welcome off",
			cfg:     ddiscord.Config{GuildID: "g1", WelcomeEnabled: "off", LogsEnabled: "", LogChannelID: "logs"},
			event:   "GUILD_MEMBER_ADD",
			guildID: "g1",
			wantEmb: 1,
		},
		{
			name:    "unbound guild ignored",
			cfg:     ddiscord.Config{GuildID: "g1", WelcomeChannelID: "welcome"},
			event:   "GUILD_MEMBER_ADD",
			guildID: "other",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			bot, rest, _ := testBot(tc.cfg)
			_ = dispatch(t, bot, tc.event, memberPayload(tc.guildID))
			if len(rest.messages) != tc.wantMsg {
				t.Fatalf("messages = %v", rest.messages)
			}
			if len(rest.embeds) != tc.wantEmb {
				t.Fatalf("embeds = %v", rest.embeds)
			}
		})
	}
}

func TestJoinToCreateVoice(t *testing.T) {
	bot, rest, _ := testBot(ddiscord.Config{GuildID: "g1", VoiceHubID: "hub", VoiceEnabled: ""})
	err := dispatch(t, bot, "VOICE_STATE_UPDATE", map[string]any{
		"guild_id": "g1", "channel_id": "hub", "user_id": "u1",
		"member": map[string]any{"user": map[string]any{"id": "u1", "username": "Ada"}},
	})
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
	open := map[string]any{
		"id": "i1", "token": "tok", "guild_id": "g1", "channel_id": "support",
		"data":   map[string]any{"custom_id": customTicketOpen},
		"member": map[string]any{"user": map[string]any{"id": "u1", "username": "Ada"}, "permissions": "8"},
	}
	if err := dispatch(t, bot, "INTERACTION_CREATE", open); err != nil {
		t.Fatal(err)
	}
	if len(rest.channels) != 1 {
		t.Fatalf("ticket channel = %v", rest.channels)
	}
	closeEv := map[string]any{
		"id": "i2", "token": "tok", "guild_id": "g1", "channel_id": rest.channels[0],
		"data":   map[string]any{"name": "ticket", "options": []any{map[string]any{"name": "close", "type": 1}}},
		"member": map[string]any{"user": map[string]any{"id": "u1", "username": "Ada"}, "permissions": "8"},
	}
	if err := dispatch(t, bot, "INTERACTION_CREATE", closeEv); err != nil {
		t.Fatal(err)
	}
	if len(rest.deleted) != 1 {
		t.Fatalf("deleted = %v", rest.deleted)
	}
}

func TestDailyAndRank(t *testing.T) {
	bot, rest, _ := testBot(ddiscord.Config{GuildID: "g1", LevelsEnabled: ""})
	daily := map[string]any{
		"id": "i1", "token": "tok", "guild_id": "g1",
		"data":   map[string]any{"name": "daily"},
		"member": map[string]any{"user": map[string]any{"id": "u1"}},
	}
	if err := dispatch(t, bot, "INTERACTION_CREATE", daily); err != nil {
		t.Fatal(err)
	}
	if len(rest.replies) != 1 {
		t.Fatalf("daily reply = %v", rest.replies)
	}
	if err := dispatch(t, bot, "INTERACTION_CREATE", daily); err != nil {
		t.Fatal(err)
	}
	if rest.replies[1] != "Already claimed today." {
		t.Fatalf("second daily = %q", rest.replies[1])
	}
}

func TestModerationRequiresPerms(t *testing.T) {
	bot, rest, _ := testBot(ddiscord.Config{GuildID: "g1"})
	kick := map[string]any{
		"id": "i1", "token": "tok", "guild_id": "g1",
		"data": map[string]any{"name": "kick", "options": []any{
			map[string]any{"name": "user", "type": 6, "value": "u2"},
		}},
		"member": map[string]any{"user": map[string]any{"id": "u1"}, "permissions": "0"},
	}
	_ = dispatch(t, bot, "INTERACTION_CREATE", kick)
	if rest.kick != 0 {
		t.Fatal("kick without perms")
	}
	kick2 := map[string]any{
		"id": "i2", "token": "tok", "guild_id": "g1",
		"data": map[string]any{"name": "kick", "options": []any{
			map[string]any{"name": "user", "type": 6, "value": "u2"},
		}},
		"member": map[string]any{"user": map[string]any{"id": "u1"}, "permissions": "8"},
	}
	_ = dispatch(t, bot, "INTERACTION_CREATE", kick2)
	if rest.kick != 1 {
		t.Fatal("admin kick")
	}
}

func TestLevelUpOnChat(t *testing.T) {
	bot, rest, mem := testBot(ddiscord.Config{GuildID: "g1", LevelsEnabled: ""})
	mem.SeedXP(store.XPSeed{Member: store.Member{GuildID: "g1", UserID: "u1"}, Amount: 90})
	err := dispatch(t, bot, "MESSAGE_CREATE", map[string]any{
		"id": "m1", "guild_id": "g1", "channel_id": "chat",
		"content": "hi",
		"author":  map[string]any{"id": "u1", "username": "Ada"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(rest.messages) != 1 {
		t.Fatalf("level-up messages = %v", rest.messages)
	}
}

func TestEmptyCloneIsDeleted(t *testing.T) {
	bot, rest, _ := testBot(ddiscord.Config{GuildID: "g1", VoiceHubID: "hub", VoiceEnabled: ""})
	voice := func(channelID string) map[string]any {
		return map[string]any{
			"guild_id": "g1", "channel_id": channelID, "user_id": "u1",
			"member": map[string]any{"user": map[string]any{"id": "u1", "username": "Ada"}},
		}
	}
	_ = dispatch(t, bot, "VOICE_STATE_UPDATE", voice("hub"))
	cloneID := rest.channels[0]
	_ = dispatch(t, bot, "VOICE_STATE_UPDATE", voice(cloneID))
	_ = dispatch(t, bot, "VOICE_STATE_UPDATE", voice(""))
	if len(rest.deleted) != 1 || rest.deleted[0] != cloneID {
		t.Fatalf("deleted = %v want %s", rest.deleted, cloneID)
	}
}

func TestReadyRegistersSlash(t *testing.T) {
	bot, rest, _ := testBot(ddiscord.Config{GuildID: "g1"})
	if err := bot.Ready(context.Background(), gateway.Identity{ApplicationID: "app-1", BotUserID: "bot-1"}); err != nil {
		t.Fatal(err)
	}
	if rest.commands != 1 {
		t.Fatalf("commands registered = %d", rest.commands)
	}
}
