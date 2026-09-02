// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package worker

import (
	"context"
	"errors"
	"strconv"
	"strings"

	discapi "ItsBagelBot/app/outgress/internal/discord"
	ddiscord "ItsBagelBot/internal/domain/discord"
	eventtwitch "ItsBagelBot/internal/domain/event/twitch"
	"ItsBagelBot/internal/projection"

	"go.uber.org/zap"
)

// discordGuildAPI is the REST slice the live announcer and guild fill need
// on top of SendMessage. *discapi.Client implements it; the chat-only
// test fake does not, so live tests inject a recorder that does.
type discordGuildAPI interface {
	discordAPI
	SendEmbed(ctx context.Context, post discapi.EmbedPost) (discapi.Message, error)
	EditMessage(ctx context.Context, m discapi.Message, patch discapi.MessagePatch) error
	CreateChannel(ctx context.Context, ch discapi.GuildChannel) (discapi.Snowflake, error)
	DeleteChannel(ctx context.Context, ch discapi.Snowflake) error
	CreateRole(ctx context.Context, role discapi.GuildRole) (discapi.Snowflake, error)
	AddMemberRole(ctx context.Context, r discapi.MemberRole) error
	RemoveMemberRole(ctx context.Context, r discapi.MemberRole) error
	ListGuildChannels(ctx context.Context, guild discapi.Guild) ([]discapi.Snowflake, error)
	ListGuildRoles(ctx context.Context, guild discapi.Guild) ([]discapi.Snowflake, error)
	GetGuild(ctx context.Context, guild discapi.Guild) (discapi.Snowflake, error)
}

// liveJob is one go-live / go-offline pass: the guild client, the module
// blob, and the Twitch id the live-message key is stored under.
type liveJob struct {
	guild         discordGuildAPI
	cfg           ddiscord.Config
	broadcasterID string
}

func (j liveJob) liveKey() liveMsgKey {
	return liveMsgKey{BroadcasterID: j.broadcasterID}
}

// clipJob is one clip-archive post after Helix Create Clip succeeds.
type clipJob struct {
	BroadcasterID string
	Embed         ddiscord.Embed
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
	cfg, enabled := w.discordConfig(ctx, discordUser{ID: status.BroadcasterID})
	if !enabled {
		return
	}
	if !cfg.Connected() {
		return
	}
	if !cfg.LiveOn() {
		return
	}
	job := liveJob{
		guild:         guild,
		cfg:           cfg,
		broadcasterID: strconv.FormatUint(status.BroadcasterID, 10),
	}
	if !status.Live {
		w.discordOffline(ctx, job)
		w.revokeLiveRole(ctx, job)
		return
	}
	w.discordOnline(ctx, job)
}

// discordOnline posts the go-live embed once per stream. A repeated
// stream.online (EventSub retry, AckWait replay, pod restart mid-handler)
// finds the remembered message and only re-asserts the live role instead of
// posting a second embed and orphaning the first.
func (w *Worker) discordOnline(ctx context.Context, job liveJob) {
	channelID := strings.TrimSpace(job.cfg.LiveChannelID)
	login := strings.TrimSpace(job.cfg.TwitchLogin)
	if channelID == "" {
		return
	}
	if login == "" {
		return
	}
	if w.liveMessageKnown(ctx, job) {
		w.grantLiveRole(ctx, job)
		return
	}
	info := w.liveInfo(ctx, job)
	if !job.cfg.CategoryAllowed(info.GameName) {
		return
	}
	if err := w.takeDiscordGlobal(ctx); err != nil {
		w.log.Warn("discord go-live embed skipped: global bucket",
			zap.String("broadcaster_id", job.broadcasterID), zap.Error(err))
		return
	}
	embed := ddiscord.LiveEmbed(ddiscord.LiveEmbedInput{
		Login:        login,
		Title:        info.Title,
		Category:     info.GameName,
		ThumbnailURL: "https://static-cdn.jtvnw.net/previews-ttv/live_user_" + strings.ToLower(login) + "-640x360.jpg",
		Viewers:      info.ViewerCount,
	})
	msg, err := job.guild.SendEmbed(ctx, discapi.EmbedPost{ChannelID: channelID, Embed: embed})
	if err != nil {
		w.log.Warn("discord go-live embed failed",
			zap.String("broadcaster_id", job.broadcasterID), zap.Error(err))
		return
	}
	if w.discordKV != nil {
		_ = w.discordKV.PutLiveMessage(ctx, job.liveKey(), msg)
	}
	w.grantLiveRole(ctx, job)
}

func (w *Worker) liveMessageKnown(ctx context.Context, job liveJob) bool {
	if w.discordKV == nil {
		return false
	}
	_, ok := w.discordKV.GetLiveMessage(ctx, job.liveKey())
	return ok
}

// liveInfo reads the projected title/category. stream.online lands on this
// lane before the stream_status job projects the metadata, so with an
// allow-list set (the one case an unknown category cannot be decided) it
// pays one system Helix call rather than silently skipping every allow-list
// user's announcement.
func (w *Worker) liveInfo(ctx context.Context, job liveJob) projection.StreamInfo {
	info, _ := w.streamInfoGet(ctx, job)
	if info.GameName != "" {
		return info
	}
	if !job.cfg.HasCategoryAllow() {
		return info
	}
	if w.twitch == nil {
		return info
	}
	if err := w.takeSystemHelix(ctx); err != nil {
		return info
	}
	details, live, err := w.twitch.StreamDetails(ctx, job.broadcasterID)
	if err != nil {
		return info
	}
	if !live {
		return info
	}
	return projection.StreamInfo{Title: details.Title, GameName: details.GameName, ViewerCount: details.ViewerCount}
}

// discordOffline edits the remembered go-live message and forgets it. A
// message the streamer deleted (404) is forgotten too, so the stale key is
// not re-edited on every later offline event.
func (w *Worker) discordOffline(ctx context.Context, job liveJob) {
	if w.discordKV == nil {
		return
	}
	msg, ok := w.discordKV.GetLiveMessage(ctx, job.liveKey())
	if !ok {
		return
	}
	err := job.guild.EditMessage(ctx, msg, discapi.MessagePatch{
		Content: ddiscord.OfflineContent,
		Embeds:  []ddiscord.Embed{},
	})
	if keepLiveMessage(err) {
		w.log.Warn("discord go-offline edit failed",
			zap.String("broadcaster_id", job.broadcasterID), zap.Error(err))
		return
	}
	_ = w.discordKV.DeleteLiveMessage(ctx, job.liveKey())
}

func keepLiveMessage(err error) bool {
	if err == nil {
		return false
	}
	return !errors.Is(err, discapi.ErrChannelNotFound)
}

func (w *Worker) grantLiveRole(ctx context.Context, job liveJob) {
	role, ok := liveRoleOf(job.cfg)
	if !ok {
		return
	}
	if err := job.guild.AddMemberRole(ctx, role); err != nil {
		w.log.Warn("discord live role update failed", zap.Error(err))
	}
}

func (w *Worker) revokeLiveRole(ctx context.Context, job liveJob) {
	role, ok := liveRoleOf(job.cfg)
	if !ok {
		return
	}
	if err := job.guild.RemoveMemberRole(ctx, role); err != nil {
		w.log.Warn("discord live role update failed", zap.Error(err))
	}
}

func liveRoleOf(cfg ddiscord.Config) (discapi.MemberRole, bool) {
	if cfg.LiveRoleID == "" {
		return discapi.MemberRole{}, false
	}
	if cfg.GuildID == "" {
		return discapi.MemberRole{}, false
	}
	if cfg.StreamerDiscordID == "" {
		return discapi.MemberRole{}, false
	}
	return discapi.MemberRole{GuildID: cfg.GuildID, UserID: cfg.StreamerDiscordID, RoleID: cfg.LiveRoleID}, true
}

func (w *Worker) discordConfig(ctx context.Context, user discordUser) (ddiscord.Config, bool) {
	src := w.discordMods
	if src == nil {
		src = w.streamInfo
	}
	if src == nil {
		return ddiscord.Config{}, false
	}
	mod, found, err := src.GetModule(ctx, user.ID, ddiscord.ModuleName)
	if err != nil {
		// A Valkey blip must be visible: silently reading as "not connected"
		// hides every skipped go-live post.
		w.log.Warn("discord module read failed; treating as not connected",
			zap.Uint64("broadcaster_id", user.ID), zap.Error(err))
		return ddiscord.Config{}, false
	}
	if !found {
		return ddiscord.Config{}, false
	}
	if !mod.IsEnabled {
		return ddiscord.Config{}, false
	}
	return ddiscord.Parse(mod.Configs), true
}

func (w *Worker) streamInfoGet(ctx context.Context, job liveJob) (projection.StreamInfo, bool) {
	if w.streamInfo == nil {
		return projection.StreamInfo{}, false
	}
	info, known, err := w.streamInfo.GetStreamInfo(ctx, job.broadcasterID)
	if err != nil {
		return projection.StreamInfo{}, false
	}
	if !known {
		return projection.StreamInfo{}, false
	}
	return info, true
}

// announceDiscordClip posts a clip archive embed. Called from processClip
// after Helix succeeds; failures never nack the clip job.
func (w *Worker) announceDiscordClip(ctx context.Context, job clipJob) {
	guild, ok := w.guildAPI()
	if !ok {
		return
	}
	if job.Embed.URL == "" {
		return
	}
	channelID, ok := w.clipsChannel(ctx, job)
	if !ok {
		return
	}
	if err := w.takeDiscordGlobal(ctx); err != nil {
		w.log.Warn("discord clip embed skipped: global bucket",
			zap.String("broadcaster_id", job.BroadcasterID), zap.Error(err))
		return
	}
	if _, err := guild.SendEmbed(ctx, discapi.EmbedPost{ChannelID: channelID, Embed: job.Embed}); err != nil {
		w.log.Warn("discord clip embed failed",
			zap.String("broadcaster_id", job.BroadcasterID), zap.Error(err))
	}
}

// clipsChannel resolves where a clip archive post goes, if anywhere.
func (w *Worker) clipsChannel(ctx context.Context, job clipJob) (string, bool) {
	id, err := strconv.ParseUint(job.BroadcasterID, 10, 64)
	if err != nil {
		return "", false
	}
	cfg, enabled := w.discordConfig(ctx, discordUser{ID: id})
	if !enabled {
		return "", false
	}
	if !cfg.Connected() {
		return "", false
	}
	if !cfg.ClipsOn() {
		return "", false
	}
	channelID := strings.TrimSpace(cfg.ClipsChannelID)
	if channelID == "" {
		return "", false
	}
	return channelID, true
}
