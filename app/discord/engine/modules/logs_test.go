// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules_test

import (
	"context"
	"strings"
	"testing"

	"ItsBagelBot/app/discord/engine/internal/decode"
	"ItsBagelBot/app/discord/engine/internal/registry"
	"ItsBagelBot/app/discord/engine/module"
	"ItsBagelBot/app/discord/engine/modules"
	"ItsBagelBot/internal/discordstore"
	ddiscord "ItsBagelBot/internal/domain/discord"
	"ItsBagelBot/pkg/codec"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type logRig struct {
	t     *testing.T
	store *discordstore.Mem
	cfg   ddiscord.Config
	reg   *registry.Registry
}

func newLogRig(t *testing.T) *logRig {
	t.Helper()
	store := discordstore.NewMem()
	return newLogRigWith(t, store, registry.New(modules.Logs(store), modules.Message(store)))
}

func newLogRigWith(t *testing.T, store *discordstore.Mem, reg *registry.Registry) *logRig {
	t.Helper()
	return &logRig{
		t: t, store: store,
		cfg: ddiscord.Config{GuildID: "g1", LogsEnabled: "on", LogChannelID: "log-all"},
		reg: reg,
	}
}

type posted struct {
	ChannelID string
	Embed     ddiscord.Embed
}

func (r *logRig) fire(eventType string, payload map[string]any) []posted {
	r.t.Helper()
	raw, err := codec.Marshal(payload)
	require.NoError(r.t, err)
	var out []posted
	c := &module.Context{Event: ddiscord.Event{Type: eventType, GuildID: "g1", Raw: raw}, Config: r.cfg, Log: zap.NewNop()}
	emit := func(cmd ddiscord.Command) {
		require.Equal(r.t, ddiscord.TypePostEmbed, cmd.Type)
		var p ddiscord.EmbedPayload
		require.NoError(r.t, codec.Unmarshal(cmd.Payload, &p))
		out = append(out, posted{ChannelID: cmd.ChannelID, Embed: p.Embed})
	}
	for _, h := range r.reg.Events(eventType) {
		require.NoError(r.t, h(context.Background(), c, emit))
	}
	return out
}

func (e posted) field(name string) string {
	for _, f := range e.Embed.Fields {
		if f.Name == name {
			return f.Value
		}
	}
	return ""
}

func message(id, content string, extra map[string]any) map[string]any {
	m := map[string]any{
		"id": id, "guild_id": "g1", "channel_id": "c1", "content": content,
		"author": map[string]any{"id": "u1", "username": "Ada"},
	}
	for k, v := range extra {
		m[k] = v
	}
	return m
}

func only(t *testing.T, got []posted) posted {
	t.Helper()
	require.Len(t, got, 1)
	return got[0]
}

func TestLogsMessageDeleteUsesTheCache(t *testing.T) {
	r := newLogRig(t)
	r.fire("MESSAGE_CREATE", message("m1", "hello there", map[string]any{
		"attachments": []map[string]any{{"url": "https://cdn/x.png"}},
	}))

	hit := only(t, r.fire("MESSAGE_DELETE", map[string]any{"id": "m1", "guild_id": "g1", "channel_id": "c1"}))
	require.Equal(t, "log-all", hit.ChannelID)
	require.Equal(t, "Message deleted", hit.Embed.Title)
	require.Equal(t, "<@u1>", hit.field("Author"))
	require.Equal(t, "<#c1>", hit.field("Channel"))
	require.Equal(t, "hello there", hit.field("Content"))
	require.Equal(t, "https://cdn/x.png", hit.field("Attachments"))

	miss := only(t, r.fire("MESSAGE_DELETE", map[string]any{"id": "gone", "guild_id": "g1", "channel_id": "c1"}))
	require.Equal(t, "Unknown author", miss.field("Author"))
	require.Equal(t, "Unknown (not cached)", miss.field("Content"))
}

func TestLogsMessageEditShowsBeforeAndAfterAndSkipsNoise(t *testing.T) {
	r := newLogRig(t)
	r.fire("MESSAGE_CREATE", message("m1", "first", nil))

	require.Empty(t, r.fire("MESSAGE_UPDATE", message("m1", "first", nil)), "unchanged content is an embed unfurl")
	require.Empty(t, r.fire("MESSAGE_UPDATE", map[string]any{"id": "m1", "guild_id": "g1", "channel_id": "c1"}), "no content means no edit")

	edit := only(t, r.fire("MESSAGE_UPDATE", message("m1", "second", nil)))
	require.Equal(t, "Message edited", edit.Embed.Title)
	require.Equal(t, "first", edit.field("Before"))
	require.Equal(t, "second", edit.field("After"))

	again := only(t, r.fire("MESSAGE_UPDATE", message("m1", "third", nil)))
	require.Equal(t, "second", again.field("Before"), "the cache follows the latest edit")
}

func TestLogsMessageEditIsIndependentOfModuleOrder(t *testing.T) {
	store := discordstore.NewMem()
	r := newLogRigWith(t, store, registry.New(modules.Message(store), modules.Logs(store)))
	r.fire("MESSAGE_CREATE", message("m1", "first", nil))

	edit := only(t, r.fire("MESSAGE_UPDATE", message("m1", "second", nil)))
	require.Equal(t, "first", edit.field("Before"))
	require.Equal(t, "second", edit.field("After"))
	again := only(t, r.fire("MESSAGE_UPDATE", message("m1", "third", nil)))
	require.Equal(t, "second", again.field("Before"))
}

func TestLogsCachedBotMessages(t *testing.T) {
	botAuthor := map[string]any{"author": map[string]any{"id": "b1", "username": "Beep", "bot": true}}
	del := map[string]any{"id": "mb", "guild_id": "g1", "channel_id": "c1"}

	t.Run("ignore bots drops a cached bot delete and edit", func(t *testing.T) {
		r := newLogRig(t)
		r.fire("MESSAGE_CREATE", message("mb", "beep", botAuthor))
		require.Empty(t, r.fire("MESSAGE_DELETE", del))
		require.Empty(t, r.fire("MESSAGE_UPDATE", map[string]any{"id": "mb", "guild_id": "g1", "channel_id": "c1", "content": "boop"}))
	})
	t.Run("ignore bots off logs the bot author", func(t *testing.T) {
		r := newLogRig(t)
		r.cfg.LogIgnoreBots = "off"
		r.fire("MESSAGE_CREATE", message("mb", "beep", botAuthor))
		got := only(t, r.fire("MESSAGE_DELETE", del))
		require.Equal(t, "<@b1>", got.field("Author"))
		require.Equal(t, "beep", got.field("Content"))
	})
	t.Run("an uncached delete still logs as unknown", func(t *testing.T) {
		r := newLogRig(t)
		require.Equal(t, "Unknown author", only(t, r.fire("MESSAGE_DELETE", del)).field("Author"))
	})
}

func TestLogsBulkDeleteCountsMessages(t *testing.T) {
	r := newLogRig(t)
	got := only(t, r.fire("MESSAGE_DELETE_BULK", map[string]any{"ids": []string{"a", "b", "c"}, "guild_id": "g1", "channel_id": "c1"}))
	require.Equal(t, "3 messages deleted in <#c1>", got.Embed.Description)
}

func TestLogsMemberJoinAndLeaveLogOnceThroughAllModules(t *testing.T) {
	store := discordstore.NewMem()
	reg := registry.New(modules.All(modules.Deps{Store: store, Log: zap.NewNop()})...)
	cfg := ddiscord.Config{GuildID: "g1", LogsEnabled: "on", LogChannelID: "log-all", WelcomeEnabled: "off"}
	for _, tc := range []struct{ event, title string }{{"GUILD_MEMBER_ADD", "Member joined"}, {"GUILD_MEMBER_REMOVE", "Member left"}} {
		raw, err := codec.Marshal(map[string]any{"guild_id": "g1", "user": map[string]any{"id": "u1", "username": "Ada"}})
		require.NoError(t, err)
		var titles []string
		c := &module.Context{Event: ddiscord.Event{Type: tc.event, GuildID: "g1", Raw: raw}, Config: cfg, Log: zap.NewNop()}
		for _, h := range reg.Events(tc.event) {
			require.NoError(t, h(context.Background(), c, func(cmd ddiscord.Command) {
				var p ddiscord.EmbedPayload
				require.NoError(t, codec.Unmarshal(cmd.Payload, &p))
				titles = append(titles, p.Embed.Title)
			}))
		}
		require.Equal(t, []string{tc.title}, titles)
	}
}

func memberUpdate(nick string, roles ...string) map[string]any {
	return map[string]any{"guild_id": "g1", "nick": nick, "roles": roles, "user": map[string]any{"id": "u1", "username": "Ada"}}
}

func TestLogsMemberUpdateReportsNickAndRoleDiffsAfterFirstSighting(t *testing.T) {
	r := newLogRig(t)
	require.Empty(t, r.fire("GUILD_MEMBER_UPDATE", memberUpdate("Ada", "r1")), "first sighting has nothing to diff")

	nick := only(t, r.fire("GUILD_MEMBER_UPDATE", memberUpdate("Adie", "r1")))
	require.Equal(t, "Nickname changed", nick.Embed.Title)
	require.Equal(t, "Ada", nick.field("Before"))
	require.Equal(t, "Adie", nick.field("After"))

	roles := only(t, r.fire("GUILD_MEMBER_UPDATE", memberUpdate("Adie", "r2", "r3")))
	require.Equal(t, "Roles changed", roles.Embed.Title)
	require.Equal(t, "<@&r2> <@&r3>", roles.field("Added"))
	require.Equal(t, "<@&r1>", roles.field("Removed"))

	require.Empty(t, r.fire("GUILD_MEMBER_UPDATE", memberUpdate("Adie", "r2", "r3")))
}

func TestLogsBanAndUnban(t *testing.T) {
	r := newLogRig(t)
	ban := map[string]any{"guild_id": "g1", "user": map[string]any{"id": "u2", "username": "Sam"}}
	require.Equal(t, "Member banned", only(t, r.fire("GUILD_BAN_ADD", ban)).Embed.Title)
	require.Equal(t, "Member unbanned", only(t, r.fire("GUILD_BAN_REMOVE", ban)).Embed.Title)
}

func TestLogsChannelsRolesAndServer(t *testing.T) {
	r := newLogRig(t)
	channel := func(name string) map[string]any {
		return map[string]any{"id": "c9", "guild_id": "g1", "name": name, "type": 0}
	}
	require.Equal(t, "Channel created", only(t, r.fire("CHANNEL_CREATE", channel("general"))).Embed.Title)
	require.Empty(t, r.fire("CHANNEL_UPDATE", channel("general")), "only renames are logged")
	renamed := only(t, r.fire("CHANNEL_UPDATE", channel("lounge")))
	require.Equal(t, "general", renamed.field("Before"))
	require.Equal(t, "lounge", renamed.field("After"))
	deleted := only(t, r.fire("CHANNEL_DELETE", channel("lounge")))
	require.Equal(t, "Channel deleted", deleted.Embed.Title)
	require.Equal(t, "lounge", deleted.Embed.Description)
	require.Equal(t, "Thread created", only(t, r.fire("THREAD_CREATE", map[string]any{"id": "t1", "guild_id": "g1", "name": "talk", "parent_id": "c9"})).Embed.Title)
	require.Equal(t, "Thread deleted", only(t, r.fire("THREAD_DELETE", map[string]any{"id": "t1", "guild_id": "g1", "parent_id": "c9"})).Embed.Title)

	role := func(name string) map[string]any {
		return map[string]any{"guild_id": "g1", "role": map[string]any{"id": "r1", "name": name}}
	}
	require.Equal(t, "Role created", only(t, r.fire("GUILD_ROLE_CREATE", role("Mods"))).Embed.Title)
	require.Equal(t, "Mods", only(t, r.fire("GUILD_ROLE_UPDATE", role("Staff"))).field("Before"))
	gone := only(t, r.fire("GUILD_ROLE_DELETE", map[string]any{"guild_id": "g1", "role_id": "r1"}))
	require.Equal(t, "Staff", gone.Embed.Description, "the deleted role name comes from the cache")
	unseen := only(t, r.fire("GUILD_ROLE_DELETE", map[string]any{"guild_id": "g1", "role_id": "r7"}))
	require.Equal(t, "r7", unseen.Embed.Description)

	require.Empty(t, r.fire("GUILD_UPDATE", map[string]any{"id": "g1", "name": "Bagels"}), "first sighting")
	server := only(t, r.fire("GUILD_UPDATE", map[string]any{"id": "g1", "name": "Bagel HQ"}))
	require.Equal(t, "Bagels", server.field("Before"))
	invite := map[string]any{"guild_id": "g1", "channel_id": "c1", "code": "abc"}
	require.Contains(t, only(t, r.fire("INVITE_CREATE", invite)).Embed.Description, "abc")
	require.Equal(t, "Invite deleted", only(t, r.fire("INVITE_DELETE", invite)).Embed.Title)
}

func TestLogsSkipTemporaryVoiceRooms(t *testing.T) {
	r := newLogRig(t)
	r.cfg.VoiceCategoryID, r.cfg.VoiceHubID = "cat1", "hub"
	room := func(id, parent string, kind int) map[string]any {
		return map[string]any{"id": id, "guild_id": "g1", "name": id, "type": kind, "parent_id": parent}
	}

	require.Empty(t, r.fire("CHANNEL_CREATE", room("clone1", "cat1", ddiscord.ChannelVoice)), "clone in the voice category")
	require.Empty(t, r.fire("CHANNEL_DELETE", room("clone1", "cat1", ddiscord.ChannelVoice)))
	require.NoError(t, r.store.TrackClone(context.Background(), discordstore.Clone{ChannelID: "clone2", GuildID: "g1", OwnerID: "u1"}))
	require.Empty(t, r.fire("CHANNEL_DELETE", room("clone2", "", ddiscord.ChannelVoice)), "tracked clone")

	require.Len(t, r.fire("CHANNEL_CREATE", room("hub", "cat1", ddiscord.ChannelVoice)), 1, "the hub itself is logged")
	require.Len(t, r.fire("CHANNEL_CREATE", room("text1", "cat1", 0)), 1, "text channels are logged")
	require.Len(t, r.fire("CHANNEL_CREATE", room("voice1", "other", ddiscord.ChannelVoice)), 1, "voice outside the category is logged")
}

func TestLogsRoutingAndSuppression(t *testing.T) {
	ban := map[string]any{"guild_id": "g1", "user": map[string]any{"id": "u2", "username": "Sam"}}
	del := map[string]any{"id": "m1", "guild_id": "g1", "channel_id": "c1"}

	t.Run("a category channel wins and the others fall back", func(t *testing.T) {
		r := newLogRig(t)
		r.cfg.LogModerationChannelID = "mod-log"
		require.Equal(t, "mod-log", only(t, r.fire("GUILD_BAN_ADD", ban)).ChannelID)
		require.Equal(t, "log-all", only(t, r.fire("MESSAGE_DELETE", del)).ChannelID)
	})
	t.Run("a category toggled off is silent", func(t *testing.T) {
		r := newLogRig(t)
		r.cfg.LogMessagesEnabled = "off"
		require.Empty(t, r.fire("MESSAGE_DELETE", del))
		require.NotEmpty(t, r.fire("GUILD_BAN_ADD", ban))
	})
	t.Run("an ignored source channel is silent", func(t *testing.T) {
		r := newLogRig(t)
		r.cfg.LogIgnoredChannels = "c1"
		require.Empty(t, r.fire("MESSAGE_DELETE", del))
		require.NotEmpty(t, r.fire("GUILD_BAN_ADD", ban))
	})
	t.Run("bots are ignored when asked", func(t *testing.T) {
		r := newLogRig(t)
		bot := map[string]any{"guild_id": "g1", "nick": "", "roles": []string{}, "user": map[string]any{"id": "b1", "bot": true}}
		join := map[string]any{"guild_id": "g1", "user": map[string]any{"id": "b1", "bot": true}}
		require.Empty(t, r.fire("GUILD_MEMBER_ADD", join))
		require.Empty(t, r.fire("GUILD_MEMBER_UPDATE", bot))
		edit := message("m2", "beep", map[string]any{"author": map[string]any{"id": "b1", "bot": true}})
		require.Empty(t, r.fire("MESSAGE_UPDATE", edit))

		r.cfg.LogIgnoreBots = "off"
		require.NotEmpty(t, r.fire("GUILD_MEMBER_ADD", join))
		require.NotEmpty(t, r.fire("MESSAGE_UPDATE", edit))
	})
	t.Run("logs off emits nothing", func(t *testing.T) {
		r := newLogRig(t)
		r.cfg.LogsEnabled = "off"
		require.Empty(t, r.fire("MESSAGE_DELETE", del))
		require.Empty(t, r.fire("GUILD_BAN_ADD", ban))
		require.Empty(t, r.fire("MESSAGE_CREATE", message("m3", "hi", nil)))
		_, cached := r.store.RecallMessage(context.Background(), discordstore.Message{ID: "m3"})
		require.False(t, cached, "nothing is cached while logs are off")
	})
	t.Run("a missing channel emits nothing", func(t *testing.T) {
		r := newLogRig(t)
		r.cfg.LogChannelID = ""
		require.Empty(t, r.fire("GUILD_BAN_ADD", ban))
	})
}

func TestMessageCacheSkipsDMsAndClipsContent(t *testing.T) {
	r := newLogRig(t)
	ctx := context.Background()
	r.fire("MESSAGE_CREATE", message("dm", "psst", map[string]any{"guild_id": ""}))
	r.fire("MESSAGE_CREATE", message("long", strings.Repeat("é", 3000), nil))

	_, dm := r.store.RecallMessage(ctx, discordstore.Message{ID: "dm"})
	long, cached := r.store.RecallMessage(ctx, discordstore.Message{ID: "long"})
	require.False(t, dm)
	require.True(t, cached)
	require.Len(t, []rune(long.Content), 1024)
	require.Equal(t, "Ada", long.AuthorName)
}

func TestLogRoutingOfModerationCommands(t *testing.T) {
	cfg := ddiscord.Config{GuildID: "g1", LogsEnabled: "on", LogChannelID: "log-all", LogModerationChannelID: "mod-log", ModsRoleID: "m"}
	in := ada.in("c1")
	in.Member.Permissions = "8"
	in.Data.Options = []decode.InteractionOption{userOption("u2")}
	cmds := runHandler(t, modules.Moderation(nil, zap.NewNop()).Slash["ban"], cfg, in)
	var channels []string
	for _, c := range cmds {
		if c.Type == ddiscord.TypePostEmbed {
			channels = append(channels, c.ChannelID)
		}
	}
	require.Equal(t, []string{"mod-log"}, channels)
}
