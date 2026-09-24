// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"context"
	"regexp"

	"ItsBagelBot/app/discord/engine/internal/cmd"
	"ItsBagelBot/app/discord/engine/internal/decode"
	"ItsBagelBot/app/discord/engine/module"
	ddiscord "ItsBagelBot/internal/domain/discord"
	"ItsBagelBot/internal/domain/discord/linkguard"

	"go.uber.org/zap"
)

type Guarder interface {
	Observe(ctx context.Context, s linkguard.Sighting) linkguard.Verdict
}

var linkPattern = regexp.MustCompile(`(?i)[a-z0-9][a-z0-9.-]*\.[a-z]{2,}(?:/\S*)?`)

func LinkGuard(guard Guarder, own OwnInviteChecker, log *zap.Logger) module.Module {
	h := linkGuardModule{guard: guard, own: own, log: log}
	b := module.NewModule("linkguard")
	b.On("MESSAGE_CREATE", h.onCreate)
	return b.Build()
}

type linkGuardModule struct {
	guard Guarder
	own   OwnInviteChecker
	log   *zap.Logger
}

func (h linkGuardModule) onCreate(ctx context.Context, c *module.Context, emit module.Emit) error {
	ev, err := decode.Decode[decode.MessageEvent](c.Event.Raw)
	if err != nil {
		return err
	}
	if skipMessage(ev, c.Config) {
		return nil
	}
	links := linkPattern.FindAllString(ev.Content, -1)
	if len(links) == 0 {
		return nil
	}
	moderator := decode.HasAnyRole(ev.Member.Roles, c.Config.StaffRoleIDs())
	in := linkObservation{Module: c, Event: ev, Links: links, Moderator: moderator}
	if reason := h.observeLinks(ctx, in); reason != "" {
		h.act(c, emit, ev, reason)
	}
	return nil
}

type linkObservation struct {
	Module    *module.Context
	Event     decode.MessageEvent
	Links     []string
	Moderator bool
}

func skipMessage(ev decode.MessageEvent, cfg ddiscord.Config) bool {
	return ev.Author.Bot || ev.GuildID == "" || !cfg.LinkGuardOn()
}

func (h linkGuardModule) observeLinks(ctx context.Context, in linkObservation) string {
	ev, c := in.Event, in.Module
	reason := ""
	seen := make(map[string]bool, len(in.Links))
	for _, raw := range in.Links {
		norm, _ := linkguard.NormalizeLink(raw)
		if norm == "" || seen[norm] {
			continue
		}
		seen[norm] = true
		v := h.guard.Observe(ctx, linkguard.Sighting{
			GuildID:   ev.GuildID,
			ChannelID: ev.ChannelID,
			UserID:    ev.Author.ID,
			MessageID: ev.ID,
			Link:      raw,
			OwnerID:   c.BroadcasterID,
			Moderator: in.Moderator,
			Allowed:   c.Config.LinkAllowed(raw),
		})
		if v.Allow || reason != "" {
			continue
		}
		if h.tripIsOwnInvite(ctx, ev.GuildID, raw, v) {
			continue
		}
		reason = v.Reason
	}
	return reason
}

func (h linkGuardModule) tripIsOwnInvite(ctx context.Context, guildID, raw string, v linkguard.Verdict) bool {
	if !v.IsInvite || h.own == nil {
		return false
	}
	own, err := h.own.IsOwnGuildInvite(ctx, guildID, raw)
	if err != nil {
		h.log.Warn("linkguard: invite resolve failed, not deleting", zap.String("guild_id", guildID), zap.Error(err))
		return true
	}
	return own
}

func (h linkGuardModule) act(c *module.Context, emit module.Emit, ev decode.MessageEvent, reason string) {
	emit(cmd.DeleteMessage(cmd.ChannelTarget(c.Config.GuildID, ev.ChannelID), ev.ID, cmd.Reason("linkguard: "+reason)))
	body := decode.Mention(ev.Author) + " in <#" + ev.ChannelID + "> (" + reason + ")"
	_ = logLine(c, emit, logEntry{Title: "Link removed", Body: body})
}
