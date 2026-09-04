// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Package setup is the guild setup/layout/unbind/post half of what used to
// live in app/dingress/internal/egress (discord_setup.go's ported home) and,
// before dingress, app/twitch/outgress/internal/worker/discord*.go. It is the one
// REST-shaped operation the task that split dingress explicitly kept here
// rather than moving to engine: filling a guild template is a sequence of
// list/create calls where each step's id feeds the next, which has nothing
// for engine to "decide" -- see internal/domain/rpc/outgress's DiscordSetupRequest,
// still the dashboard's unchanged wire contract.
package setup

import (
	"context"
	"unicode/utf8"

	discapi "ItsBagelBot/internal/discordapi"
	"ItsBagelBot/internal/discordstore"

	"go.uber.org/zap"
)

// discordGuildAPI is the REST slice every handler in this package fires
// through. discordrate.LimitedClient satisfies this directly.
type discordGuildAPI interface {
	SendChat(ctx context.Context, post discapi.ChatPost) error
	SendPanel(ctx context.Context, post discapi.EmbedPost, buttons []discapi.Button) (discapi.Message, error)
	CreateChannel(ctx context.Context, ch discapi.GuildChannel) (discapi.Snowflake, error)
	CreateRole(ctx context.Context, role discapi.GuildRole) (discapi.Snowflake, error)
	ListGuildChannels(ctx context.Context, guild discapi.Guild) ([]discapi.Snowflake, error)
	ListGuildRoles(ctx context.Context, guild discapi.Guild) ([]discapi.Snowflake, error)
}

// Worker holds everything the setup/layout/unbind/post RPC needs.
type Worker struct {
	discord discordGuildAPI
	store   discordstore.Store
	log     *zap.Logger
}

// Config wires a Worker's dependencies.
type Config struct {
	Discord discordGuildAPI
	Store   discordstore.Store
	Log     *zap.Logger
}

// New builds a Worker.
func New(cfg Config) *Worker {
	log := cfg.Log
	if log == nil {
		log = zap.NewNop()
	}
	return &Worker{discord: cfg.Discord, store: cfg.Store, log: log}
}

// PostDiscord is the operator/home-server path (changelog, status posts). It
// skips no gate: the content bound and the shared global bucket both still
// apply, because one misbehaving caller must not be able to trip the bot's
// global 429 for the whole fleet.
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
// runes.
const discordContentMaxRunes = 2000

func discordContentOK(content string) bool {
	return content != "" && utf8.RuneCountInString(content) <= discordContentMaxRunes
}
