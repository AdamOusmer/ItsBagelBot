// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package worker

import (
	"context"
	"strconv"
	"strings"
	"time"

	discapi "ItsBagelBot/app/outgress/internal/discord"
	ddiscord "ItsBagelBot/internal/domain/discord"
	eventtwitch "ItsBagelBot/internal/domain/event/twitch"
	"ItsBagelBot/internal/projection"

	"github.com/valkey-io/valkey-go"
	"go.uber.org/zap"
)

// discordGuildAPI is the REST slice the live announcer and guild fill need
// on top of SendMessage. *discapi.Client implements it; the chat-only
// test fake does not, so live tests inject a recorder that does.
type discordGuildAPI interface {
	discordAPI
	SendEmbed(ctx context.Context, channelID, content string, embed ddiscord.Embed) (string, error)
	EditMessage(ctx context.Context, channelID, messageID, content string, embeds []ddiscord.Embed) error
	CreateChannel(ctx context.Context, guildID string, ch discapi.ChannelCreate) (discapi.Snowflake, error)
	DeleteChannel(ctx context.Context, channelID string) error
	CreateRole(ctx context.Context, guildID string, role discapi.RoleCreate) (discapi.Snowflake, error)
	AddMemberRole(ctx context.Context, guildID, userID, roleID string) error
	RemoveMemberRole(ctx context.Context, guildID, userID, roleID string) error
	ListGuildChannels(ctx context.Context, guildID string) ([]discapi.Snowflake, error)
	ListGuildRoles(ctx context.Context, guildID string) ([]discapi.Snowflake, error)
	GetGuild(ctx context.Context, guildID string) (discapi.Snowflake, error)
}

// discordModuleReader is the GetModule slice live/clip announcers need.
// *projection.Store implements it; tests inject a map.
type discordModuleReader interface {
	GetModule(ctx context.Context, userID uint64, name string) (projection.ModuleView, bool, error)
}

// discordLiveStore remembers the go-live message so stream.offline can
// edit it, and the guild→Twitch reverse index dingress needs. Production
// uses Valkey; tests use a map.
type discordLiveStore interface {
	PutLiveMessage(ctx context.Context, broadcasterID, channelID, messageID string) error
	GetLiveMessage(ctx context.Context, broadcasterID string) (channelID, messageID string, ok bool)
	PutGuild(ctx context.Context, guildID, broadcasterID string) error
}

type valkeyDiscordLive struct {
	client valkey.Client
}

func NewDiscordLiveStore(client valkey.Client) discordLiveStore {
	return newValkeyDiscordLive(client)
}

func newValkeyDiscordLive(client valkey.Client) discordLiveStore {
	if client == nil {
		return nil
	}
	return valkeyDiscordLive{client: client}
}

func discordLiveKey(broadcasterID string) string { return "discord:live-msg:" + broadcasterID }

func (s valkeyDiscordLive) PutLiveMessage(ctx context.Context, broadcasterID, channelID, messageID string) error {
	return s.client.Do(ctx, s.client.B().Set().Key(discordLiveKey(broadcasterID)).
		Value(channelID+"|"+messageID).Ex(36*time.Hour).Build()).Error()
}

func (s valkeyDiscordLive) GetLiveMessage(ctx context.Context, broadcasterID string) (string, string, bool) {
	raw, err := s.client.Do(ctx, s.client.B().Get().Key(discordLiveKey(broadcasterID)).Build()).ToString()
	if err != nil {
		return "", "", false
	}
	ch, msg, ok := strings.Cut(raw, "|")
	return ch, msg, ok && ch != "" && msg != ""
}

func discordGuildKey(guildID string) string { return "discord:guild:" + guildID }

func (s valkeyDiscordLive) PutGuild(ctx context.Context, guildID, broadcasterID string) error {
	if guildID == "" || broadcasterID == "" {
		return nil
	}
	return s.client.Do(ctx, s.client.B().Set().Key(discordGuildKey(guildID)).
		Value(broadcasterID).Build()).Error()
}

// HandleStreamEvent also posts the Discord go-live / go-offline embed.
// This is the load-bearing product rule: live events never pass through
// sesame. The handler already acks always; Discord failures only log.
func (w *Worker) announceDiscordLive(ctx context.Context, status eventtwitch.StreamStatus) {
	if w.discord == nil {
		return
	}
	guild, ok := w.discord.(discordGuildAPI)
	if !ok {
		return
	}
	broadcasterID := strconv.FormatUint(status.BroadcasterID, 10)
	cfg, enabled := w.discordConfig(ctx, status.BroadcasterID)
	if !enabled || !cfg.Connected() || !cfg.LiveOn() {
		return
	}
	if !status.Live {
		w.discordOffline(ctx, guild, broadcasterID)
		w.discordLiveRole(ctx, guild, cfg, false)
		return
	}

	info, _ := w.streamInfoGet(ctx, broadcasterID)
	if !cfg.CategoryAllowed(info.GameName) {
		return
	}
	channelID := strings.TrimSpace(cfg.LiveChannelID)
	if channelID == "" {
		return
	}
	login := cfg.TwitchLogin
	thumb := ""
	if login != "" {
		thumb = "https://static-cdn.jtvnw.net/previews-ttv/live_user_" + strings.ToLower(login) + "-640x360.jpg"
	}
	embed := ddiscord.LiveEmbed(login, info.Title, info.GameName, thumb, info.ViewerCount)
	msgID, err := guild.SendEmbed(ctx, channelID, "", embed)
	if err != nil {
		w.log.Warn("discord go-live embed failed",
			zap.String("broadcaster_id", broadcasterID), zap.Error(err))
		return
	}
	if w.discordKV != nil && msgID != "" {
		_ = w.discordKV.PutLiveMessage(ctx, broadcasterID, channelID, msgID)
	}
	w.discordLiveRole(ctx, guild, cfg, true)
}

func (w *Worker) discordOffline(ctx context.Context, guild discordGuildAPI, broadcasterID string) {
	if w.discordKV == nil {
		return
	}
	ch, msg, ok := w.discordKV.GetLiveMessage(ctx, broadcasterID)
	if !ok {
		return
	}
	if err := guild.EditMessage(ctx, ch, msg, ddiscord.OfflineContent, []ddiscord.Embed{}); err != nil {
		w.log.Warn("discord go-offline edit failed",
			zap.String("broadcaster_id", broadcasterID), zap.Error(err))
	}
}

func (w *Worker) discordLiveRole(ctx context.Context, guild discordGuildAPI, cfg ddiscord.Config, live bool) {
	if cfg.LiveRoleID == "" || cfg.GuildID == "" || cfg.StreamerDiscordID == "" {
		return
	}
	var err error
	if live {
		err = guild.AddMemberRole(ctx, cfg.GuildID, cfg.StreamerDiscordID, cfg.LiveRoleID)
	} else {
		err = guild.RemoveMemberRole(ctx, cfg.GuildID, cfg.StreamerDiscordID, cfg.LiveRoleID)
	}
	if err != nil {
		w.log.Warn("discord live role update failed", zap.Error(err))
	}
}

func (w *Worker) discordConfig(ctx context.Context, broadcasterID uint64) (ddiscord.Config, bool) {
	src := w.discordMods
	if src == nil && w.streamInfo != nil {
		src = w.streamInfo
	}
	if src == nil {
		return ddiscord.Config{}, false
	}
	mod, found, err := src.GetModule(ctx, broadcasterID, ddiscord.ModuleName)
	if err != nil || !found || !mod.IsEnabled {
		return ddiscord.Config{}, false
	}
	return ddiscord.Parse(mod.Configs), true
}

func (w *Worker) streamInfoGet(ctx context.Context, broadcasterID string) (projection.StreamInfo, bool) {
	if w.streamInfo == nil {
		return projection.StreamInfo{}, false
	}
	info, known, err := w.streamInfo.GetStreamInfo(ctx, broadcasterID)
	if err != nil || !known {
		return projection.StreamInfo{}, false
	}
	return info, true
}

// announceDiscordClip posts a clip archive embed. Called from processClip
// after Helix succeeds; failures never nack the clip job.
func (w *Worker) announceDiscordClip(ctx context.Context, broadcasterID, clipURL, clipper, title string) {
	if w.discord == nil || clipURL == "" {
		return
	}
	id, err := strconv.ParseUint(broadcasterID, 10, 64)
	if err != nil {
		return
	}
	cfg, enabled := w.discordConfig(ctx, id)
	if !enabled || !cfg.Connected() || !cfg.ClipsOn() {
		return
	}
	channelID := strings.TrimSpace(cfg.ClipsChannelID)
	if channelID == "" {
		return
	}
	guild, ok := w.discord.(discordGuildAPI)
	if !ok {
		_ = w.discord.SendMessage(ctx, channelID, clipURL, false)
		return
	}
	embed := ddiscord.ClipEmbed(clipURL, clipper, title)
	if _, err := guild.SendEmbed(ctx, channelID, "", embed); err != nil {
		w.log.Warn("discord clip embed failed",
			zap.String("broadcaster_id", broadcasterID), zap.Error(err))
	}
}
