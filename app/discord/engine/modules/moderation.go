// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"context"
	"strconv"
	"time"

	"ItsBagelBot/app/discord/engine/internal/cmd"
	"ItsBagelBot/app/discord/engine/internal/decode"
	"ItsBagelBot/app/discord/engine/module"
	ddiscord "ItsBagelBot/internal/domain/discord"
	discordoutgress "ItsBagelBot/internal/domain/rpc/discordoutgress"

	"go.uber.org/zap"
)

// purgeClient is the one RPC call this module needs (see
// internal/domain/rpc/discordoutgress's doc for why /purge cannot be a
// Command: outgress has to list before it can bulk-delete, and the count
// deleted is exactly the thing the slash reply needs to report).
type purgeClient interface {
	Purge(ctx context.Context, req discordoutgress.PurgeRequest) (discordoutgress.PurgeReply, error)
}

// Moderation ports app/dingress/internal/community/slash.go's /timeout,
// /kick, /ban, and /purge. Every action here lands on Command's mod lane
// (see internal/domain/discord's ModType) except purge, which is REST-shaped
// (see purgeClient above) and answers through the same RPC round trip
// instead.
func Moderation(purge purgeClient, log *zap.Logger) module.Module {
	h := moderationModule{purgeRPC: purge, log: log}
	b := module.NewModule("moderation")
	b.Slash("timeout", h.timeout)
	b.Slash("kick", h.kick)
	b.Slash("ban", h.ban)
	b.Slash("purge", h.purge)
	return b.Build()
}

type moderationModule struct {
	purgeRPC purgeClient
	log      *zap.Logger
}

// requireMod replies "Mods only." and reports false when the interacting
// member lacks a moderation-capable permission bit.
func requireMod(c *module.Context, in decode.InteractionEvent, emit module.Emit) bool {
	if decode.CanMod(in.Member.Permissions) {
		return true
	}
	emit(cmd.Followup(c.Config.GuildID, in.Token, "Mods only.", true))
	return false
}

func (h moderationModule) timeout(_ context.Context, c *module.Context, emit module.Emit) error {
	in, err := decode.Decode[decode.InteractionEvent](c.Event.Raw)
	if err != nil {
		return err
	}
	if !requireMod(c, in, emit) {
		return nil
	}
	userID := decode.OptionUser(in.Data.Options, "user")
	mins := decode.OptionIntFrom(in.Data.Options, "minutes")
	if userID == "" || mins <= 0 {
		emit(cmd.Followup(c.Config.GuildID, in.Token, "Need a user and a duration in minutes.", true))
		return nil
	}
	until := time.Now().UTC().Add(time.Duration(mins) * time.Minute).Format(time.RFC3339)
	emit(cmd.TimeoutMember(in.GuildID, userID, until, "slash /timeout"))
	line := decode.Mention(decode.UserRef{ID: userID}) + " for " + strconv.Itoa(mins) + " minutes"
	_ = logLine(c, emit, logEntry{Title: "Timeout", Body: line})
	emit(cmd.Followup(c.Config.GuildID, in.Token, "Timed out "+line+".", true))
	return nil
}

func (h moderationModule) kick(_ context.Context, c *module.Context, emit module.Emit) error {
	return h.remove(c, emit, "Kick", "Kicked ", cmd.KickMember)
}

func (h moderationModule) ban(_ context.Context, c *module.Context, emit module.Emit) error {
	return h.remove(c, emit, "Ban", "Banned ", cmd.BanMember)
}

// removeBuilder is cmd.KickMember or cmd.BanMember's shared shape.
type removeBuilder func(guildID, userID, reason string) ddiscord.Command

// remove is modTimeout's kick/ban twin: same mods-only gate, same "need a
// user" validation, same log-then-reply tail, differing only in which
// mod-lane Command it builds and what it says.
func (h moderationModule) remove(c *module.Context, emit module.Emit, title, prefix string, build removeBuilder) error {
	in, err := decode.Decode[decode.InteractionEvent](c.Event.Raw)
	if err != nil {
		return err
	}
	if !requireMod(c, in, emit) {
		return nil
	}
	userID := decode.OptionUser(in.Data.Options, "user")
	if userID == "" {
		emit(cmd.Followup(c.Config.GuildID, in.Token, "Need a user.", true))
		return nil
	}
	emit(build(in.GuildID, userID, "slash /"+title))
	who := decode.Mention(decode.UserRef{ID: userID})
	_ = logLine(c, emit, logEntry{Title: title, Body: who})
	emit(cmd.Followup(c.Config.GuildID, in.Token, prefix+who+".", true))
	return nil
}

func (h moderationModule) purge(ctx context.Context, c *module.Context, emit module.Emit) error {
	in, err := decode.Decode[decode.InteractionEvent](c.Event.Raw)
	if err != nil {
		return err
	}
	if !requireMod(c, in, emit) {
		return nil
	}
	n := decode.OptionIntFrom(in.Data.Options, "count")
	n = clampPurgeCount(n)
	reply, err := h.purgeRPC.Purge(ctx, discordoutgress.PurgeRequest{ChannelID: in.ChannelID, Count: n})
	if err != nil {
		h.log.Warn("purge rpc failed", zap.Error(err))
		emit(cmd.Followup(c.Config.GuildID, in.Token, "Purge failed.", true))
		return nil
	}
	if reply.Error != "" {
		emit(cmd.Followup(c.Config.GuildID, in.Token, "Purge failed.", true))
		return nil
	}
	if reply.Deleted < 2 {
		emit(cmd.Followup(c.Config.GuildID, in.Token, "Not enough messages to purge.", true))
		return nil
	}
	_ = logLine(c, emit, logEntry{Title: "Purge", Body: strconv.Itoa(reply.Deleted) + " messages in <#" + in.ChannelID + ">"})
	emit(cmd.Followup(c.Config.GuildID, in.Token, "Deleted "+strconv.Itoa(reply.Deleted)+" messages.", true))
	return nil
}

func clampPurgeCount(n int) int {
	if n < 2 {
		n = 2
	}
	if n > 100 {
		n = 100
	}
	return n
}
