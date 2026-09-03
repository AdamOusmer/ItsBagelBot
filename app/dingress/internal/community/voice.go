// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package community

import (
	"context"
	"strconv"

	"ItsBagelBot/app/dingress/internal/store"
	"ItsBagelBot/internal/discordapi"
	ddiscord "ItsBagelBot/internal/domain/discord"
)

func (b *Bot) onVoice(ctx context.Context, raw []byte) error {
	ev, err := decode[voiceEvent](raw)
	if err != nil {
		return err
	}
	cfg, ok := b.bound(ctx, store.Guild{ID: ev.GuildID})
	if !ok {
		return nil
	}
	if ev.UserID == b.botUser {
		return nil
	}
	left, leftEmpty := b.occ.update(voiceSeat{GuildID: ev.GuildID, UserID: ev.UserID, ChannelID: ev.ChannelID})
	if leftEmpty {
		_ = b.deleteEmptyClone(ctx, store.Channel{ID: left})
	}
	if !cfg.VoiceOn() {
		return nil
	}
	if ev.ChannelID == "" {
		return nil
	}
	if ev.ChannelID != cfg.VoiceHubID {
		return nil
	}
	return b.cloneAndMove(ctx, ev)
}

func (b *Bot) cloneAndMove(ctx context.Context, ev voiceEvent) error {
	if b.Store.CloneCount(ctx, store.Guild{ID: ev.GuildID}) >= ddiscord.VoiceCloneCap {
		return nil
	}
	name := displayName(display{User: ev.Member.User})
	if name == "" {
		name = "voice"
	}
	ch, err := b.REST.CreateChannel(ctx, discordapi.GuildChannel{
		Guild: discordapi.Guild{ID: ev.GuildID},
		Spec: discordapi.ChannelCreate{
			Name: name,
			Type: ddiscord.ChannelVoice,
			PermissionOverwrites: []discordapi.PermissionOverwrite{
				overwriteAllow(overwriteSpec{TargetID: ev.UserID, Kind: 1, Bits: permView | permConnect | permSend}),
			},
		},
	})
	if err != nil {
		return err
	}
	if err := b.Store.TrackClone(ctx, store.Clone{ChannelID: ch.ID, GuildID: ev.GuildID, OwnerID: ev.UserID}); err != nil {
		return err
	}
	return b.REST.MoveMember(ctx, discordapi.VoiceMove{
		GuildID: ev.GuildID, UserID: ev.UserID, ChannelID: ch.ID,
	})
}

func (b *Bot) deleteEmptyClone(ctx context.Context, ch store.Channel) error {
	if ch.ID == "" {
		return nil
	}
	cl, ok := b.Store.Clone(ctx, ch)
	if !ok {
		return nil
	}
	_ = b.Store.ForgetClone(ctx, cl)
	return b.REST.DeleteChannel(ctx, discordapi.Snowflake{ID: ch.ID})
}

func (b *Bot) voiceCommand(ctx context.Context, in interactionEvent, sub interactionOption) error {
	cl, ok := b.Store.Clone(ctx, store.Channel{ID: in.ChannelID})
	if !ok {
		return b.reply(ctx, in, "You can only do that in a temporary voice channel.")
	}
	if !ownsVoice(cl, in) {
		return b.reply(ctx, in, "Only the channel owner can do that.")
	}
	return b.applyVoice(ctx, in, cl, sub)
}

func ownsVoice(cl store.Clone, in interactionEvent) bool {
	if cl.OwnerID == in.Member.User.ID {
		return true
	}
	return canMod(permBits{Raw: in.Member.Permissions})
}

func (b *Bot) applyVoice(ctx context.Context, in interactionEvent, cl store.Clone, sub interactionOption) error {
	switch sub.Name {
	case "name":
		return b.renameVoice(ctx, in, cl, sub)
	case "limit":
		return b.limitVoice(ctx, in, cl, sub)
	case "lock":
		return b.lockVoice(ctx, in, cl, true)
	case "unlock":
		return b.lockVoice(ctx, in, cl, false)
	default:
		return b.reply(ctx, in, "Unknown voice command.")
	}
}

func (b *Bot) renameVoice(ctx context.Context, in interactionEvent, cl store.Clone, sub interactionOption) error {
	name := optionString(sub, "name")
	if name == "" {
		return b.reply(ctx, in, "Give the channel a name.")
	}
	err := b.REST.ModifyChannel(ctx, discordapi.ChannelPatch{ID: cl.ChannelID, Name: name})
	if err != nil {
		return err
	}
	return b.reply(ctx, in, "Renamed.")
}

func (b *Bot) limitVoice(ctx context.Context, in interactionEvent, cl store.Clone, sub interactionOption) error {
	n := optionInt(sub, "count")
	err := b.REST.ModifyChannel(ctx, discordapi.ChannelPatch{ID: cl.ChannelID, UserLimit: n})
	if err != nil {
		return err
	}
	return b.reply(ctx, in, "User limit set to "+strconv.Itoa(n)+".")
}

func (b *Bot) lockVoice(ctx context.Context, in interactionEvent, cl store.Clone, lock bool) error {
	overwrites := []discordapi.PermissionOverwrite{
		overwriteAllow(overwriteSpec{TargetID: cl.OwnerID, Kind: 1, Bits: permView | permConnect}),
	}
	if lock {
		overwrites = append(overwrites, overwriteDeny(overwriteSpec{TargetID: cl.GuildID, Kind: 0, Bits: permConnect}))
	}
	err := b.REST.ModifyChannel(ctx, discordapi.ChannelPatch{ID: cl.ChannelID, PermissionOverwrites: overwrites})
	if err != nil {
		return err
	}
	msg := "Unlocked."
	if lock {
		msg = "Locked."
	}
	return b.reply(ctx, in, msg)
}
