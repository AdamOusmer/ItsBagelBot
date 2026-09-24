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

type purgeClient interface {
	Purge(ctx context.Context, req discordoutgress.PurgeRequest) (discordoutgress.PurgeReply, error)
}

func Moderation(purge purgeClient, log *zap.Logger) module.Module {
	h := moderationModule{purgeRPC: purge, log: log}
	b := module.NewModule("moderation")
	b.Slash("timeout", h.timeout)
	b.Slash("kick", h.kick)
	b.Slash("ban", h.ban)
	b.Slash("purge", h.purge)
	return b.Build()
}

// Must read IsModStaff, not IsTicketStaff: ticket desk roles must not gain ban power.
func isStaffOrMod(cfg ddiscord.Config, in decode.InteractionEvent) bool {
	return decode.CanMod(in.Member.Permissions) || ddiscord.IsModStaff(in.Member.Roles, cfg)
}

func isTicketStaffOrMod(cfg ddiscord.Config, in decode.InteractionEvent) bool {
	return decode.CanMod(in.Member.Permissions) || ddiscord.IsTicketStaff(in.Member.Roles, cfg)
}

type moderationModule struct {
	purgeRPC purgeClient
	log      *zap.Logger
}

func requireMod(c *module.Context, in decode.InteractionEvent, emit module.Emit) bool {
	if isStaffOrMod(c.Config, in) {
		return true
	}
	emit(cmd.Followup(cmd.GuildTarget(c.Config.GuildID), cmd.Token(in.Token), "Mods only.", true))
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
		emit(cmd.Followup(cmd.GuildTarget(c.Config.GuildID), cmd.Token(in.Token), "Need a user and a duration in minutes.", true))
		return nil
	}
	until := time.Now().UTC().Add(time.Duration(mins) * time.Minute).Format(time.RFC3339)
	emit(cmd.TimeoutMember(cmd.UserTarget(in.GuildID, userID), until, "slash /timeout"))
	line := decode.Mention(decode.UserRef{ID: userID}) + " for " + strconv.Itoa(mins) + " minutes"
	_ = logLine(c, emit, logEntry{Title: "Timeout", Body: line})
	emit(cmd.Followup(cmd.GuildTarget(c.Config.GuildID), cmd.Token(in.Token), "Timed out "+line+".", true))
	return nil
}

func (h moderationModule) kick(_ context.Context, c *module.Context, emit module.Emit) error {
	return h.remove(c, emit, removeAction{Title: "Kick", Prefix: "Kicked ", Build: cmd.KickMember})
}

func (h moderationModule) ban(_ context.Context, c *module.Context, emit module.Emit) error {
	return h.remove(c, emit, removeAction{Title: "Ban", Prefix: "Banned ", Build: cmd.BanMember})
}

type removeBuilder func(t cmd.Target, reason cmd.Reason) ddiscord.Command

type removeAction struct {
	Title  string
	Prefix string
	Build  removeBuilder
}

func (h moderationModule) remove(c *module.Context, emit module.Emit, action removeAction) error {
	in, err := decode.Decode[decode.InteractionEvent](c.Event.Raw)
	if err != nil {
		return err
	}
	if !requireMod(c, in, emit) {
		return nil
	}
	userID := decode.OptionUser(in.Data.Options, "user")
	if userID == "" {
		emit(cmd.Followup(cmd.GuildTarget(c.Config.GuildID), cmd.Token(in.Token), "Need a user.", true))
		return nil
	}
	emit(action.Build(cmd.UserTarget(in.GuildID, userID), cmd.Reason("slash /"+action.Title)))
	who := decode.Mention(decode.UserRef{ID: userID})
	_ = logLine(c, emit, logEntry{Title: action.Title, Body: who})
	emit(cmd.Followup(cmd.GuildTarget(c.Config.GuildID), cmd.Token(in.Token), action.Prefix+who+".", true))
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
		emit(cmd.Followup(cmd.GuildTarget(c.Config.GuildID), cmd.Token(in.Token), "Purge failed.", true))
		return nil
	}
	if reply.Error != "" {
		emit(cmd.Followup(cmd.GuildTarget(c.Config.GuildID), cmd.Token(in.Token), "Purge failed.", true))
		return nil
	}
	if reply.Deleted < 2 {
		emit(cmd.Followup(cmd.GuildTarget(c.Config.GuildID), cmd.Token(in.Token), "Not enough messages to purge.", true))
		return nil
	}
	_ = logLine(c, emit, logEntry{Title: "Purge", Body: strconv.Itoa(reply.Deleted) + " messages in <#" + in.ChannelID + ">"})
	emit(cmd.Followup(cmd.GuildTarget(c.Config.GuildID), cmd.Token(in.Token), "Deleted "+strconv.Itoa(reply.Deleted)+" messages.", true))
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
