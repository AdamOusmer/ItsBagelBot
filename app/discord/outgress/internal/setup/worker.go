// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package setup

import (
	"context"
	"unicode/utf8"

	discapi "ItsBagelBot/internal/discordapi"
	"ItsBagelBot/internal/discordstore"

	"go.uber.org/zap"
)

type discordGuildAPI interface {
	SendChat(ctx context.Context, post discapi.ChatPost) error
	SendPanel(ctx context.Context, post discapi.EmbedPost, buttons []discapi.Button) (discapi.Message, error)
	DeleteMessage(ctx context.Context, m discapi.Message) error
	CreateChannel(ctx context.Context, ch discapi.GuildChannel) (discapi.Snowflake, error)
	CreateRole(ctx context.Context, role discapi.GuildRole) (discapi.Snowflake, error)
	ListGuildChannels(ctx context.Context, guild discapi.Guild) ([]discapi.Snowflake, error)
	ListGuildRoles(ctx context.Context, guild discapi.Guild) ([]discapi.Snowflake, error)
	GetGuildWithCounts(ctx context.Context, guild discapi.Guild) (discapi.GuildInfo, error)
}

type Worker struct {
	discord discordGuildAPI
	store   discordstore.Store
	log     *zap.Logger
}

type Config struct {
	Discord discordGuildAPI
	Store   discordstore.Store
	Log     *zap.Logger
}

func New(cfg Config) *Worker {
	log := cfg.Log
	if log == nil {
		log = zap.NewNop()
	}
	return &Worker{discord: cfg.Discord, store: cfg.Store, log: log}
}

func (w *Worker) PostDiscord(ctx context.Context, channelID, content string) error {
	if w.discord == nil {
		return discapi.ErrAuth
	}
	if channelID == "" || !discordContentOK(content) {
		return discapi.ErrBadRequest
	}
	return w.discord.SendChat(ctx, discapi.ChatPost{ChannelID: channelID, Content: content})
}

const discordContentMaxRunes = 2000

func discordContentOK(content string) bool {
	return content != "" && utf8.RuneCountInString(content) <= discordContentMaxRunes
}
