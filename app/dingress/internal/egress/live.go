// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package egress

import (
	"context"
	"errors"
	"strconv"
	"strings"

	discapi "ItsBagelBot/internal/discordapi"
	ddiscord "ItsBagelBot/internal/domain/discord"
	eventtwitch "ItsBagelBot/internal/domain/event/twitch"
	"ItsBagelBot/internal/projection"
	"ItsBagelBot/pkg/bus"

	"go.uber.org/zap"
)

// liveJob is one go-live / go-offline pass: the module blob and the Twitch
// id the live-message key is stored under. Ported from outgress's liveJob;
// it no longer carries its own REST client (w.discord is already typed
// discordGuildAPI here, so there is nothing to type-assert the way
// outgress's guildAPI() did against its narrower discordAPI field).
type liveJob struct {
	cfg           ddiscord.Config
	broadcasterID string
}

func (j liveJob) liveKey() liveMsgKey {
	return liveMsgKey{BroadcasterID: j.broadcasterID}
}

// clipJob is one clip-archive post after Helix Create Clip succeeded on
// outgress and published data.twitch.clip.created.
type clipJob struct {
	BroadcasterID string
	Embed         ddiscord.Embed
}

// HandleStreamEvent decodes one twitch.ingress.event.stream message and
// posts the Discord go-live / go-offline embed. This is the load-bearing
// product rule carried over from outgress: live events never pass through
// sesame. Egress binds its OWN durable consumer on this subject (see
// app/dingress/main.go) as a plain second subscriber alongside outgress and
// the projector -- every one of them gets every event once. Always returns
// nil (ack): a malformed or unrelated event is dropped, and a Discord
// failure only logs -- there is nothing to retry that a redelivery would
// fix differently.
func (w *Worker) HandleStreamEvent(msg *bus.Message) error {
	status, ok := eventtwitch.DecodeStreamStatus(msg.Payload)
	if !ok {
		return nil
	}
	w.announceDiscordLive(msg.Context(), status)
	return nil
}

// announceDiscordLive is HandleStreamEvent's body, split out so tests can
// drive it directly with a context they control (outgress also re-verified
// mod status and sent a reauth beacon from this same event; both are
// Twitch-token concerns with no Discord involvement, so they stay on
// outgress and are not ported here).
func (w *Worker) announceDiscordLive(ctx context.Context, status eventtwitch.StreamStatus) {
	if w.discord == nil {
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
	job := liveJob{cfg: cfg, broadcasterID: strconv.FormatUint(status.BroadcasterID, 10)}
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
	embed := ddiscord.LiveEmbed(ddiscord.LiveEmbedInput{
		Login:        login,
		Title:        info.Title,
		Category:     info.GameName,
		ThumbnailURL: "https://static-cdn.jtvnw.net/previews-ttv/live_user_" + strings.ToLower(login) + "-640x360.jpg",
		Viewers:      info.ViewerCount,
	})
	msg, err := w.discord.SendEmbed(ctx, discapi.EmbedPost{ChannelID: channelID, Embed: embed})
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

// liveInfo reads the projected title/category. stream.online can land on
// this lane before the stream_status job projects the metadata, so a fresh
// go-live may see an empty GameName -- with no category allow-list that is
// fine (CategoryAllowed("") is true), and with one set the post is skipped
// for this event rather than posted with a wrong (empty) category.
//
// outgress's version had a second path here: an allow-listed broadcaster
// with no projected category yet paid one Helix StreamDetails call to
// decide instead of skipping. Egress has no Twitch client (ROLE=egress only
// gets DISCORD_BOT_TOKEN, VALKEY_*, and NATS_* -- see app/dingress/internal/
// config), so that fallback cannot be ported: an allow-listed category on a
// stream that just went live may miss its post if the title/category
// projection has not landed yet. This matches the plain (non-allow-list)
// behavior that already existed for every other broadcaster.
func (w *Worker) liveInfo(ctx context.Context, job liveJob) projection.StreamInfo {
	if w.streamInfo == nil {
		return projection.StreamInfo{}
	}
	info, known, err := w.streamInfo.GetStreamInfo(ctx, job.broadcasterID)
	if err != nil || !known {
		return projection.StreamInfo{}
	}
	return info
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
	err := w.discord.EditMessage(ctx, msg, discapi.MessagePatch{
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
	if err := w.discord.AddMemberRole(ctx, role); err != nil {
		w.log.Warn("discord live role update failed", zap.Error(err))
	}
}

func (w *Worker) revokeLiveRole(ctx context.Context, job liveJob) {
	role, ok := liveRoleOf(job.cfg)
	if !ok {
		return
	}
	if err := w.discord.RemoveMemberRole(ctx, role); err != nil {
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
