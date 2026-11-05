// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package setup

import (
	"context"
	"errors"
	"time"
	"unicode/utf8"

	discapi "ItsBagelBot/internal/discordapi"
	"ItsBagelBot/internal/discordstore"
	"ItsBagelBot/pkg/cache"

	"go.uber.org/zap"
)

type discordGuildAPI interface {
	discapi.ChannelReader
	SendChat(ctx context.Context, post discapi.ChatPost) error
	SendPanel(ctx context.Context, post discapi.EmbedPost, buttons []discapi.Button) (discapi.Message, error)
	DeleteMessage(ctx context.Context, m discapi.Message) error
	CreateChannel(ctx context.Context, ch discapi.GuildChannel) (discapi.Snowflake, error)
	CreateRole(ctx context.Context, role discapi.GuildRole) (discapi.Snowflake, error)
	ListGuildChannels(ctx context.Context, guild discapi.Guild) ([]discapi.Snowflake, error)
	ListGuildRoles(ctx context.Context, guild discapi.Guild) ([]discapi.Snowflake, error)
	GetGuildWithCounts(ctx context.Context, guild discapi.Guild) (discapi.GuildInfo, error)
}

const (
	channelGuildCapacity int64 = 4096
	channelGuildTTL            = time.Minute
)

type Worker struct {
	discord       discordGuildAPI
	store         discordstore.Store
	log           *zap.Logger
	botID         string
	channelGuilds *cache.Cache[string]
}

type Config struct {
	Discord discordGuildAPI
	Store   discordstore.Store
	Log     *zap.Logger
	BotID   string
}

func New(cfg Config) *Worker {
	log := cfg.Log
	if log == nil {
		log = zap.NewNop()
	}
	return &Worker{
		discord: cfg.Discord, store: cfg.Store, log: log, botID: cfg.BotID,
		channelGuilds: cache.New[string](channelGuildCapacity, channelGuildTTL),
	}
}

type PostRequest struct {
	BroadcasterID string
	GuildID       string
	ChannelID     string
	Content       string
}

// A recipe names its own guild and channel, so both are checked against the broadcaster on every post.
func (w *Worker) PostDiscord(ctx context.Context, req PostRequest) error {
	if w.discord == nil {
		return discapi.ErrAuth
	}
	if req.ChannelID == "" || !discordContentOK(req.Content) {
		return discapi.ErrBadRequest
	}
	if err := w.requireOwnerStrict(ctx, GuildSetupRequest{GuildID: req.GuildID, BroadcasterID: req.BroadcasterID}, ownerCheck{}); err != nil {
		return err
	}
	if err := w.requirePostChannel(ctx, req); err != nil {
		return err
	}
	err := w.discord.SendChat(ctx, discapi.ChatPost{ChannelID: req.ChannelID, Content: req.Content, NoMentions: true})
	if errors.Is(err, discapi.ErrChannelNotFound) {
		w.channelGuilds.Invalidate(req.ChannelID)
	}
	return err
}

func (w *Worker) requirePostChannel(ctx context.Context, req PostRequest) error {
	if guild, ok := w.channelGuilds.Get(req.ChannelID); ok && guild == req.GuildID {
		return nil
	}
	if err := discapi.RequireGuildChannel(ctx, w.discord, req.GuildID, req.ChannelID); err != nil {
		return err
	}
	w.channelGuilds.Set(req.ChannelID, req.GuildID)
	return nil
}

const discordContentMaxRunes = 2000

func discordContentOK(content string) bool {
	return content != "" && utf8.RuneCountInString(content) <= discordContentMaxRunes
}
