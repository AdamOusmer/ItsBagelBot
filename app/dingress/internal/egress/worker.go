// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Package egress is dingress's ROLE=egress half: no gateway Identify
// session, no NATS in ROLE=gateway. It consumes twitch.ingress.event.stream
// for go-live/offline embeds and data.twitch.clip.created for the clip
// archive post, and serves the guild setup/layout/unbind/post RPC. Every
// REST call it makes pays the same shared Valkey bucket ROLE=gateway pays
// (see internal/discordrate), because Discord's global limit is per bot
// token and both roles share one token.
//
// This package is the outbound half of what used to live in
// app/outgress/internal/worker (discord.go, discord_live.go,
// discord_setup.go, discord_kv.go) and app/outgress/rpc/discord.go. The
// behavior is ported; the file layout is not -- outgress split "chat lane
// worker with Discord bolted on" where dingress splits by Discord concern
// (live announcer / guild setup / clip announcer / RPC), matching how the
// rest of app/dingress/internal is organized (community/{welcome,ticket,
// voice,message,slash}.go).
package egress

import (
	"context"
	"unicode/utf8"

	discapi "ItsBagelBot/internal/discordapi"
	ddiscord "ItsBagelBot/internal/domain/discord"
	"ItsBagelBot/internal/projection"

	"go.uber.org/zap"
)

// discordGuildAPI is the REST slice every egress handler fires through:
// PostDiscord and the live announcer need SendChat/SendEmbed/EditMessage;
// the guild fill needs the rest. discordrate.LimitedClient (the shared
// rate-limited wrapper both dingress roles use) satisfies this directly.
type discordGuildAPI interface {
	SendChat(ctx context.Context, post discapi.ChatPost) error
	SendEmbed(ctx context.Context, post discapi.EmbedPost) (discapi.Message, error)
	SendPanel(ctx context.Context, post discapi.EmbedPost, buttons []discapi.Button) (discapi.Message, error)
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

// discordModuleReader is the GetModule slice the live/clip announcers read
// the per-broadcaster Discord config from. *projection.Store implements it
// in production; tests inject a map-backed fake.
type discordModuleReader interface {
	GetModule(ctx context.Context, userID uint64, name string) (projection.ModuleView, bool, error)
}

// streamInfoReader is the GetStreamInfo slice the live announcer reads the
// projected title/category from. *projection.Store implements it too --
// production wires the SAME store to both DiscordMods and StreamInfo.
type streamInfoReader interface {
	GetStreamInfo(ctx context.Context, userID string) (projection.StreamInfo, bool, error)
}

// discordUser is one Twitch user id the Discord module is keyed by.
type discordUser struct {
	ID uint64
}

// Worker holds everything an egress handler needs: the rate-limited REST
// client, the live-message/guild-bind store, and the module reader. Unlike
// outgress's worker.Worker, there is no Twitch client and no lane/lease
// machinery here -- egress never calls Helix, so liveInfo (below) only ever
// reads the projected title/category and never falls back to a live Helix
// call the way outgress's did.
type Worker struct {
	discord     discordGuildAPI
	discordKV   liveStore
	discordMods discordModuleReader
	streamInfo  streamInfoReader
	log         *zap.Logger
}

// Config wires a Worker's dependencies.
type Config struct {
	Discord     discordGuildAPI
	DiscordKV   liveStore
	DiscordMods discordModuleReader
	StreamInfo  streamInfoReader
	Log         *zap.Logger
}

// New builds a Worker. Discord/DiscordKV/DiscordMods/StreamInfo may all be
// nil (tests exercise pieces in isolation); production always sets every
// field.
func New(cfg Config) *Worker {
	log := cfg.Log
	if log == nil {
		log = zap.NewNop()
	}
	return &Worker{
		discord: cfg.Discord, discordKV: cfg.DiscordKV,
		discordMods: cfg.DiscordMods, streamInfo: cfg.StreamInfo, log: log,
	}
}

// discordConfig reads the enabled Discord module for a Twitch user id.
// Ported unchanged from outgress's worker.discordConfig: the three checks
// (module found, module enabled, GuildID set) are what every call site
// below needs, in the order that lets each one bail with the cheapest
// check first.
func (w *Worker) discordConfig(ctx context.Context, user discordUser) (ddiscord.Config, bool) {
	if w.discordMods == nil {
		return ddiscord.Config{}, false
	}
	mod, found, err := w.discordMods.GetModule(ctx, user.ID, ddiscord.ModuleName)
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

// PostDiscord is the operator/home-server path (changelog, status posts).
// It skips no gate: the content bound and the shared global bucket both
// still apply, because one misbehaving caller must not be able to trip the
// bot's global 429 for the whole fleet.
func (w *Worker) PostDiscord(ctx context.Context, channelID, content string) error {
	if w.discord == nil {
		return discapi.ErrAuth
	}
	if channelID == "" || !discordContentOK(content) {
		return discapi.ErrBadRequest
	}
	return w.discord.SendChat(ctx, discapi.ChatPost{ChannelID: channelID, Content: content})
}

// discordContentMaxRunes is Discord's own 2000-character limit, measured in
// runes (see the ported discord.go doc for why bytes was the wrong bound).
const discordContentMaxRunes = 2000

func discordContentOK(content string) bool {
	return content != "" && utf8.RuneCountInString(content) <= discordContentMaxRunes
}
