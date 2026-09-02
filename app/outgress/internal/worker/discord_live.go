// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package worker

import (
	"context"
	"errors"
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

// liveMessageTTL bounds how long the go-live message id is remembered. It
// is refreshed on every go-live touch, so it only has to outlast one
// stream: 7 days covers a subathon, where the first cut's 36 h left the
// post stuck on LIVE.
const liveMessageTTL = 7 * 24 * time.Hour

// discordGuildAPI is the REST slice the live announcer and guild fill need
// on top of SendMessage. *discapi.Client implements it; the chat-only
// test fake does not, so live tests inject a recorder that does.
type discordGuildAPI interface {
	discordAPI
	SendEmbed(ctx context.Context, post discapi.EmbedPost) (discapi.Message, error)
	EditMessage(ctx context.Context, m discapi.Message, content string, embeds []ddiscord.Embed) error
	CreateChannel(ctx context.Context, guildID string, ch discapi.ChannelCreate) (discapi.Snowflake, error)
	DeleteChannel(ctx context.Context, channelID string) error
	CreateRole(ctx context.Context, guildID string, role discapi.RoleCreate) (discapi.Snowflake, error)
	AddMemberRole(ctx context.Context, r discapi.MemberRole) error
	RemoveMemberRole(ctx context.Context, r discapi.MemberRole) error
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
	PutLiveMessage(ctx context.Context, broadcasterID string, m discapi.Message) error
	GetLiveMessage(ctx context.Context, broadcasterID string) (discapi.Message, bool)
	DeleteLiveMessage(ctx context.Context, broadcasterID string) error
	PutGuild(ctx context.Context, guildID, broadcasterID string) error
	GetGuild(ctx context.Context, guildID string) (broadcasterID string, ok bool)
	DeleteGuild(ctx context.Context, guildID string) error
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

func (s valkeyDiscordLive) PutLiveMessage(ctx context.Context, broadcasterID string, m discapi.Message) error {
	return s.client.Do(ctx, s.client.B().Set().Key(discordLiveKey(broadcasterID)).
		Value(m.ChannelID+"|"+m.ID).Ex(liveMessageTTL).Build()).Error()
}

func (s valkeyDiscordLive) GetLiveMessage(ctx context.Context, broadcasterID string) (discapi.Message, bool) {
	raw, err := s.client.Do(ctx, s.client.B().Get().Key(discordLiveKey(broadcasterID)).Build()).ToString()
	if err != nil {
		return discapi.Message{}, false
	}
	ch, id, ok := strings.Cut(raw, "|")
	if !ok || ch == "" || id == "" {
		return discapi.Message{}, false
	}
	return discapi.Message{ChannelID: ch, ID: id}, true
}

func (s valkeyDiscordLive) DeleteLiveMessage(ctx context.Context, broadcasterID string) error {
	return s.client.Do(ctx, s.client.B().Del().Key(discordLiveKey(broadcasterID)).Build()).Error()
}

func discordGuildKey(guildID string) string { return "discord:guild:" + guildID }

func (s valkeyDiscordLive) PutGuild(ctx context.Context, guildID, broadcasterID string) error {
	if guildID == "" || broadcasterID == "" {
		return nil
	}
	return s.client.Do(ctx, s.client.B().Set().Key(discordGuildKey(guildID)).
		Value(broadcasterID).Build()).Error()
}

func (s valkeyDiscordLive) GetGuild(ctx context.Context, guildID string) (string, bool) {
	raw, err := s.client.Do(ctx, s.client.B().Get().Key(discordGuildKey(guildID)).Build()).ToString()
	if err != nil || raw == "" {
		return "", false
	}
	return raw, true
}

func (s valkeyDiscordLive) DeleteGuild(ctx context.Context, guildID string) error {
	return s.client.Do(ctx, s.client.B().Del().Key(discordGuildKey(guildID)).Build()).Error()
}

// guildAPI returns the attached client when it speaks the guild slice.
func (w *Worker) guildAPI() (discordGuildAPI, bool) {
	if w.discord == nil {
		return nil, false
	}
	guild, ok := w.discord.(discordGuildAPI)
	return guild, ok
}

// HandleStreamEvent also posts the Discord go-live / go-offline embed.
// This is the load-bearing product rule: live events never pass through
// sesame. The handler already acks always; Discord failures only log.
func (w *Worker) announceDiscordLive(ctx context.Context, status eventtwitch.StreamStatus) {
	guild, ok := w.guildAPI()
	if !ok {
		return
	}
	cfg, enabled := w.discordConfig(ctx, status.BroadcasterID)
	if !enabled || !cfg.Connected() || !cfg.LiveOn() {
		return
	}
	broadcasterID := strconv.FormatUint(status.BroadcasterID, 10)
	if !status.Live {
		w.discordOffline(ctx, guild, broadcasterID)
		w.discordLiveRole(ctx, guild, cfg, false)
		return
	}
	w.discordOnline(ctx, guild, cfg, broadcasterID)
}

// discordOnline posts the go-live embed once per stream. A repeated
// stream.online (EventSub retry, AckWait replay, pod restart mid-handler)
// finds the remembered message and only re-asserts the live role instead of
// posting a second embed and orphaning the first.
func (w *Worker) discordOnline(ctx context.Context, guild discordGuildAPI, cfg ddiscord.Config, broadcasterID string) {
	channelID := strings.TrimSpace(cfg.LiveChannelID)
	login := strings.TrimSpace(cfg.TwitchLogin)
	if channelID == "" || login == "" {
		return
	}
	if w.liveMessageKnown(ctx, broadcasterID) {
		w.discordLiveRole(ctx, guild, cfg, true)
		return
	}
	info := w.liveInfo(ctx, cfg, broadcasterID)
	if !cfg.CategoryAllowed(info.GameName) {
		return
	}
	if err := w.takeDiscordGlobal(ctx); err != nil {
		w.log.Warn("discord go-live embed skipped: global bucket",
			zap.String("broadcaster_id", broadcasterID), zap.Error(err))
		return
	}
	embed := ddiscord.LiveEmbed(ddiscord.LiveEmbedInput{
		Login:        login,
		Title:        info.Title,
		Category:     info.GameName,
		ThumbnailURL: "https://static-cdn.jtvnw.net/previews-ttv/live_user_" + strings.ToLower(login) + "-640x360.jpg",
		Viewers:      info.ViewerCount,
	})
	msg, err := guild.SendEmbed(ctx, discapi.EmbedPost{ChannelID: channelID, Embed: embed})
	if err != nil {
		w.log.Warn("discord go-live embed failed",
			zap.String("broadcaster_id", broadcasterID), zap.Error(err))
		return
	}
	if w.discordKV != nil {
		_ = w.discordKV.PutLiveMessage(ctx, broadcasterID, msg)
	}
	w.discordLiveRole(ctx, guild, cfg, true)
}

func (w *Worker) liveMessageKnown(ctx context.Context, broadcasterID string) bool {
	if w.discordKV == nil {
		return false
	}
	_, ok := w.discordKV.GetLiveMessage(ctx, broadcasterID)
	return ok
}

// liveInfo reads the projected title/category. stream.online lands on this
// lane before the stream_status job projects the metadata, so with an
// allow-list set (the one case an unknown category cannot be decided) it
// pays one system Helix call rather than silently skipping every allow-list
// user's announcement.
func (w *Worker) liveInfo(ctx context.Context, cfg ddiscord.Config, broadcasterID string) projection.StreamInfo {
	info, known := w.streamInfoGet(ctx, broadcasterID)
	if (known && info.GameName != "") || !cfg.HasCategoryAllow() || w.twitch == nil {
		return info
	}
	if err := w.takeSystemHelix(ctx); err != nil {
		return info
	}
	details, live, err := w.twitch.StreamDetails(ctx, broadcasterID)
	if err != nil || !live {
		return info
	}
	return projection.StreamInfo{Title: details.Title, GameName: details.GameName, ViewerCount: details.ViewerCount}
}

// discordOffline edits the remembered go-live message and forgets it. A
// message the streamer deleted (404) is forgotten too, so the stale key is
// not re-edited on every later offline event.
func (w *Worker) discordOffline(ctx context.Context, guild discordGuildAPI, broadcasterID string) {
	if w.discordKV == nil {
		return
	}
	msg, ok := w.discordKV.GetLiveMessage(ctx, broadcasterID)
	if !ok {
		return
	}
	err := guild.EditMessage(ctx, msg, ddiscord.OfflineContent, []ddiscord.Embed{})
	if err != nil && !errors.Is(err, discapi.ErrChannelNotFound) {
		w.log.Warn("discord go-offline edit failed",
			zap.String("broadcaster_id", broadcasterID), zap.Error(err))
		return
	}
	_ = w.discordKV.DeleteLiveMessage(ctx, broadcasterID)
}

func (w *Worker) discordLiveRole(ctx context.Context, guild discordGuildAPI, cfg ddiscord.Config, live bool) {
	if cfg.LiveRoleID == "" || cfg.GuildID == "" || cfg.StreamerDiscordID == "" {
		return
	}
	role := discapi.MemberRole{GuildID: cfg.GuildID, UserID: cfg.StreamerDiscordID, RoleID: cfg.LiveRoleID}
	var err error
	if live {
		err = guild.AddMemberRole(ctx, role)
	} else {
		err = guild.RemoveMemberRole(ctx, role)
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
	if err != nil {
		// A Valkey blip must be visible: silently reading as "not connected"
		// hides every skipped go-live post.
		w.log.Warn("discord module read failed; treating as not connected",
			zap.Uint64("broadcaster_id", broadcasterID), zap.Error(err))
		return ddiscord.Config{}, false
	}
	if !found || !mod.IsEnabled {
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
func (w *Worker) announceDiscordClip(ctx context.Context, broadcasterID string, embed ddiscord.Embed) {
	guild, ok := w.guildAPI()
	if !ok || embed.URL == "" {
		return
	}
	channelID, ok := w.clipsChannel(ctx, broadcasterID)
	if !ok {
		return
	}
	if err := w.takeDiscordGlobal(ctx); err != nil {
		w.log.Warn("discord clip embed skipped: global bucket",
			zap.String("broadcaster_id", broadcasterID), zap.Error(err))
		return
	}
	if _, err := guild.SendEmbed(ctx, discapi.EmbedPost{ChannelID: channelID, Embed: embed}); err != nil {
		w.log.Warn("discord clip embed failed",
			zap.String("broadcaster_id", broadcasterID), zap.Error(err))
	}
}

// clipsChannel resolves where a clip archive post goes, if anywhere.
func (w *Worker) clipsChannel(ctx context.Context, broadcasterID string) (string, bool) {
	id, err := strconv.ParseUint(broadcasterID, 10, 64)
	if err != nil {
		return "", false
	}
	cfg, enabled := w.discordConfig(ctx, id)
	if !enabled || !cfg.Connected() || !cfg.ClipsOn() {
		return "", false
	}
	channelID := strings.TrimSpace(cfg.ClipsChannelID)
	return channelID, channelID != ""
}
