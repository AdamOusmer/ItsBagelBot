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
	cfg, ok := b.bound(ctx, ev.GuildID)
	if !ok {
		return nil
	}
	if ev.UserID == b.botUser {
		return nil
	}
	left, leftEmpty := b.occ.update(ev.GuildID, ev.UserID, ev.ChannelID)
	if leftEmpty {
		_ = b.deleteEmptyClone(ctx, left)
	}
	if cfg.VoiceOn() && ev.ChannelID == cfg.VoiceHubID && cfg.VoiceHubID != "" {
		return b.cloneAndMove(ctx, ev)
	}
	return nil
}

func (b *Bot) cloneAndMove(ctx context.Context, ev voiceEvent) error {
	if b.Store.CloneCount(ctx, ev.GuildID) >= ddiscord.VoiceCloneCap {
		return nil
	}
	name := displayName(ev.Member.User, "")
	if name == "" {
		name = "voice"
	}
	ch, err := b.REST.CreateChannel(ctx, discordapi.GuildChannel{
		Guild: discordapi.Guild{ID: ev.GuildID},
		Spec: discordapi.ChannelCreate{
			Name: name,
			Type: ddiscord.ChannelVoice,
			PermissionOverwrites: []discordapi.PermissionOverwrite{
				overwriteAllow(ev.UserID, 1, permView|permConnect|permSend),
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

func (b *Bot) deleteEmptyClone(ctx context.Context, channelID string) error {
	if channelID == "" {
		return nil
	}
	cl, ok := b.Store.Clone(ctx, channelID)
	if !ok {
		return nil
	}
	_ = b.Store.ForgetClone(ctx, cl)
	return b.REST.DeleteChannel(ctx, discordapi.Snowflake{ID: channelID})
}

func (b *Bot) voiceCommand(ctx context.Context, in interactionEvent, sub interactionOption) error {
	cl, ok := b.Store.Clone(ctx, in.ChannelID)
	if !ok {
		return b.reply(ctx, in, "You can only do that in a temporary voice channel.")
	}
	if cl.OwnerID != in.Member.User.ID && !canMod(in.Member.Permissions) {
		return b.reply(ctx, in, "Only the channel owner can do that.")
	}
	switch sub.Name {
	case "name":
		name := optionString(sub, "name")
		if name == "" {
			return b.reply(ctx, in, "Give the channel a name.")
		}
		err := b.REST.ModifyChannel(ctx, discordapi.ChannelPatch{ID: cl.ChannelID, Name: name})
		if err != nil {
			return err
		}
		return b.reply(ctx, in, "Renamed.")
	case "limit":
		n := optionInt(sub, "count")
		err := b.REST.ModifyChannel(ctx, discordapi.ChannelPatch{ID: cl.ChannelID, UserLimit: n})
		if err != nil {
			return err
		}
		return b.reply(ctx, in, "User limit set to "+strconv.Itoa(n)+".")
	case "lock":
		return b.lockVoice(ctx, in, cl, true)
	case "unlock":
		return b.lockVoice(ctx, in, cl, false)
	}
	return b.reply(ctx, in, "Unknown voice command.")
}

func (b *Bot) lockVoice(ctx context.Context, in interactionEvent, cl store.Clone, lock bool) error {
	everyone := cl.GuildID
	overwrites := []discordapi.PermissionOverwrite{
		overwriteAllow(cl.OwnerID, 1, permView|permConnect),
	}
	if lock {
		overwrites = append(overwrites, overwriteDeny(everyone, 0, permConnect))
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
