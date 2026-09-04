// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"context"
	"strconv"
	"strings"

	eventtwitch "ItsBagelBot/internal/domain/event/twitch"
	discordoutgress "ItsBagelBot/internal/domain/rpc/discordoutgress"

	ddiscord "ItsBagelBot/internal/domain/discord"
	"ItsBagelBot/internal/projection"
	"ItsBagelBot/pkg/bus"

	"go.uber.org/zap"
)

// liveRPC is the go-live/offline half of rpcclient.Client. See
// internal/domain/rpc/discordoutgress's doc: outgress owns whether a
// stream's go-live embed already exists, because that answer is keyed on a
// message id only outgress's own SendEmbed call ever learns.
type liveRPC interface {
	LiveOnline(ctx context.Context, req discordoutgress.LiveOnlineRequest) (discordoutgress.LiveOnlineReply, error)
	LiveOffline(ctx context.Context, req discordoutgress.LiveOfflineRequest) (discordoutgress.LiveOfflineReply, error)
}

// streamInfoFallback resolves a broadcaster's live title/category when the
// projection has not caught up yet. See liveInfo's doc below.
type streamInfoFallback interface {
	Lookup(ctx context.Context, broadcasterID string) (projection.StreamInfo, bool)
}

// streamInfoReader reads the projected title/category. *projection.Store
// implements it.
type streamInfoReader interface {
	GetStreamInfo(ctx context.Context, userID string) (projection.StreamInfo, bool, error)
}

// Publish emits one Command onto its lane-appropriate outgress subject. main
// wires this to the shared publisher (see app/discord/engine's dispatcher),
// so live/clip -- which are driven off Twitch subjects, not a Discord
// gateway event, and so never pass through the module.Builder dispatch --
// share the exact same publish path every other module's Emit ends up at.
type Publish func(ctx context.Context, c ddiscord.Command) error

// ByBroadcaster resolves a Twitch broadcaster id's Discord module config.
// Matches resolve.Resolver.ByBroadcaster's signature so main can pass that
// method directly.
type ByBroadcaster func(ctx context.Context, broadcasterID uint64) (ddiscord.Config, bool)

// Live ports app/dingress/internal/egress/live.go's go-live/offline embed
// and @Live role handling. It keeps live.go's own NATS input
// (twitch.ingress.event.stream): deciding whether to post is a decision, so
// it belongs in engine even though nothing about it is Discord-gateway-shaped.
type Live struct {
	Resolve    ByBroadcaster
	StreamInfo streamInfoReader
	Fallback   streamInfoFallback
	RPC        liveRPC
	Log        *zap.Logger
}

// HandleStreamEvent decodes one twitch.ingress.event.stream message. Always
// returns nil (ack): a malformed or unrelated event is dropped, and a
// Discord failure only logs -- there is nothing to retry that a redelivery
// would fix differently, matching the pre-split handler.
func (l *Live) HandleStreamEvent(msg *bus.Message) error {
	status, ok := eventtwitch.DecodeStreamStatus(msg.Payload)
	if !ok {
		return nil
	}
	l.announce(msg.Context(), status)
	return nil
}

func (l *Live) announce(ctx context.Context, status eventtwitch.StreamStatus) {
	cfg, ok := l.Resolve(ctx, status.BroadcasterID)
	if !ok || !cfg.Connected() || !cfg.LiveOn() {
		return
	}
	broadcasterID := strconv.FormatUint(status.BroadcasterID, 10)
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
	if err != nil || reply.Error != "" {
		l.Log.Warn("discord go-live rpc failed", zap.String("broadcaster_id", broadcasterID), zap.Error(err), zap.String("outgress_error", reply.Error))
		return
	}
}

// liveInfo mirrors egress's liveInfo unchanged: the projection is read
// first, and the Twitch-outgress RPC fallback only fires for a broadcaster
// with a category allow-list whose projection has not caught up, so an
// unfiltered broadcaster base cannot turn every go-live into a round trip.
func (l *Live) liveInfo(ctx context.Context, cfg ddiscord.Config, broadcasterID string) projection.StreamInfo {
	info := l.projectedStreamInfo(ctx, broadcasterID)
	if info.GameName != "" || !cfg.HasCategoryAllow() || l.Fallback == nil {
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
	if err != nil || reply.Error != "" {
		l.Log.Warn("discord go-offline rpc failed", zap.String("guild_id", cfg.GuildID), zap.Error(err), zap.String("outgress_error", reply.Error))
	}
}
