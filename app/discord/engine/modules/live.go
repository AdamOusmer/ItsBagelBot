// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"context"
	"strconv"
	"strings"

	eventtwitch "ItsBagelBot/internal/domain/event/twitch"
	discordoutgress "ItsBagelBot/internal/domain/rpc/discordoutgress"

	"ItsBagelBot/internal/discordstore"
	ddiscord "ItsBagelBot/internal/domain/discord"
	"ItsBagelBot/internal/projection"
	"ItsBagelBot/pkg/bus"

	"go.uber.org/zap"
)

type liveRPC interface {
	LiveOnline(ctx context.Context, req discordoutgress.LiveOnlineRequest) (discordoutgress.LiveOnlineReply, error)
	LiveOffline(ctx context.Context, req discordoutgress.LiveOfflineRequest) (discordoutgress.LiveOfflineReply, error)
}

type streamInfoFallback interface {
	Lookup(ctx context.Context, broadcasterID string) (projection.StreamInfo, bool)
}

type streamInfoReader interface {
	GetStreamInfo(ctx context.Context, userID string) (projection.StreamInfo, bool, error)
}

type Publish func(ctx context.Context, c ddiscord.Command) error

type ByBroadcaster func(ctx context.Context, broadcasterID uint64) []discordstore.GuildConfigOf

type Live struct {
	Resolve    ByBroadcaster
	StreamInfo streamInfoReader
	Fallback   streamInfoFallback
	RPC        liveRPC
	Log        *zap.Logger
}

func (l *Live) HandleStreamEvent(msg *bus.Message) error {
	status, ok := eventtwitch.DecodeStreamStatus(msg.Payload)
	if !ok {
		return nil
	}
	l.announce(msg.Context(), status)
	return nil
}

func rpcFailed(err error, outgressErr string) bool {
	return err != nil || outgressErr != ""
}

func moduleGateOpen(cfg ddiscord.Config, moduleOn bool) bool {
	return cfg.Connected() && moduleOn
}

func (l *Live) announce(ctx context.Context, status eventtwitch.StreamStatus) {
	broadcasterID := strconv.FormatUint(status.BroadcasterID, 10)
	for _, guild := range l.Resolve(ctx, status.BroadcasterID) {
		l.announceIn(ctx, guild.Config, status, broadcasterID)
	}
}

func (l *Live) announceIn(ctx context.Context, cfg ddiscord.Config, status eventtwitch.StreamStatus, broadcasterID string) {
	if !moduleGateOpen(cfg, cfg.LiveOn()) {
		return
	}
	if !status.Live {
		l.offline(ctx, cfg)
		return
	}
	l.online(ctx, cfg, broadcasterID)
}

func (l *Live) online(ctx context.Context, cfg ddiscord.Config, broadcasterID string) {
	channelID := strings.TrimSpace(cfg.LiveChannelID)
	login := strings.TrimSpace(cfg.TwitchLogin)
	if channelID == "" || login == "" {
		return
	}
	info := l.liveInfo(ctx, cfg, broadcasterID)
	if !cfg.CategoryAllowed(info.GameName) {
		return
	}
	embed := ddiscord.LiveEmbed(ddiscord.LiveEmbedInput{
		Login: login, Title: info.Title, Category: info.GameName,
		ThumbnailURL: "https://static-cdn.jtvnw.net/previews-ttv/live_user_" + strings.ToLower(login) + "-640x360.jpg",
		Viewers:      info.ViewerCount,
	})
	reply, err := l.RPC.LiveOnline(ctx, discordoutgress.LiveOnlineRequest{GuildID: cfg.GuildID, ChannelID: channelID, Embed: embed})
	if rpcFailed(err, reply.Error) {
		l.Log.Warn("discord go-live rpc failed", zap.String("broadcaster_id", broadcasterID), zap.Error(err), zap.String("outgress_error", reply.Error))
		return
	}
}

func (l *Live) liveInfo(ctx context.Context, cfg ddiscord.Config, broadcasterID string) projection.StreamInfo {
	info := l.projectedStreamInfo(ctx, broadcasterID)
	if info.GameName != "" {
		return info
	}
	if !cfg.HasCategoryAllow() {
		return info
	}
	if l.Fallback == nil {
		return info
	}
	if fallback, ok := l.Fallback.Lookup(ctx, broadcasterID); ok {
		return fallback
	}
	return info
}

func (l *Live) projectedStreamInfo(ctx context.Context, broadcasterID string) projection.StreamInfo {
	if l.StreamInfo == nil {
		return projection.StreamInfo{}
	}
	info, known, err := l.StreamInfo.GetStreamInfo(ctx, broadcasterID)
	if err != nil || !known {
		return projection.StreamInfo{}
	}
	return info
}

func (l *Live) offline(ctx context.Context, cfg ddiscord.Config) {
	reply, err := l.RPC.LiveOffline(ctx, discordoutgress.LiveOfflineRequest{GuildID: cfg.GuildID})
	if rpcFailed(err, reply.Error) {
		l.Log.Warn("discord go-offline rpc failed", zap.String("guild_id", cfg.GuildID), zap.Error(err), zap.String("outgress_error", reply.Error))
	}
}
