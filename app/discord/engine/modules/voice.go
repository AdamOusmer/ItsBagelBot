// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"context"
	"strconv"

	"ItsBagelBot/app/discord/engine/internal/cmd"
	"ItsBagelBot/app/discord/engine/internal/decode"
	"ItsBagelBot/app/discord/engine/module"
	"ItsBagelBot/internal/discordapi"
	"ItsBagelBot/internal/discordstore"
	ddiscord "ItsBagelBot/internal/domain/discord"
	discordoutgress "ItsBagelBot/internal/domain/rpc/discordoutgress"

	"go.uber.org/zap"
)

type voiceClient interface {
	channelClient
	ModifyChannel(ctx context.Context, req discordoutgress.ChannelModifyRequest) (discordoutgress.ChannelModifyReply, error)
	MoveMember(ctx context.Context, req discordoutgress.MemberMoveRequest) (discordoutgress.MemberMoveReply, error)
}

func Voice(store discordstore.Store, channels voiceClient, log *zap.Logger) module.Module {
	h := voiceModule{store: store, channels: channels, log: log}
	b := module.NewModule("voice")
	b.On("VOICE_STATE_UPDATE", h.onVoiceState)
	b.Slash("voice", h.slash)
	b.Button(discordapi.CustomVoiceLock, h.lockButton)
	b.Button(discordapi.CustomVoiceUnlock, h.unlockButton)
	return b.Build()
}

type voiceModule struct {
	store    discordstore.Store
	channels voiceClient
	log      *zap.Logger
}

func (h voiceModule) onVoiceState(ctx context.Context, c *module.Context, emit module.Emit) error {
	ev, err := decode.Decode[decode.VoiceEvent](c.Event.Raw)
	if err != nil {
		return err
	}
	if ev.Member.User.Bot {
		return nil
	}
	left, leftEmpty := h.store.UpdateVoiceOccupancy(ctx, discordstore.VoiceSeat{
		GuildID: ev.GuildID, UserID: ev.UserID, ChannelID: ev.ChannelID,
	})
	if leftEmpty {
		h.deleteEmptyClone(ctx, left)
	}
	if !joinedVoiceHub(c.Config, ev.ChannelID) {
		return nil
	}
	h.cloneAndMove(ctx, c, ev, emit)
	return nil
}

func joinedVoiceHub(cfg ddiscord.Config, channelID string) bool {
	return cfg.VoiceOn() && channelID != "" && channelID == cfg.VoiceHubID
}

func (h voiceModule) cloneAndMove(ctx context.Context, c *module.Context, ev decode.VoiceEvent, emit module.Emit) {
	if h.store.CloneCount(ctx, discordstore.Guild{ID: ev.GuildID}) >= ddiscord.VoiceCloneCap {
		return
	}
	name := decode.DisplayName(decode.Display{User: ev.Member.User})
	if name == "" {
		name = "voice"
	}
	reply, err := h.channels.CreateChannel(ctx, discordoutgress.ChannelCreateRequest{
		GuildID: ev.GuildID, Name: name, Type: ddiscord.ChannelVoice,
		Overwrites: []discordapi.PermissionOverwrite{
			decode.OverwriteAllow(decode.OverwriteSpec{TargetID: ev.UserID, Kind: 1, Bits: decode.PermView | decode.PermConnect | decode.PermSend}),
		},
	})
	if rpcFailed(err, reply.Error) {
		h.log.Warn("voice clone create failed", zap.Error(err), zap.String("outgress_error", reply.Error))
		return
	}
	if err := h.store.TrackClone(ctx, discordstore.Clone{ChannelID: reply.ChannelID, GuildID: ev.GuildID, OwnerID: ev.UserID}); err != nil {
		h.log.Warn("voice clone tracking failed", zap.Error(err))
		return
	}
	emit(cmd.PostPanel(cmd.ChannelTarget(ev.GuildID, reply.ChannelID), "", ddiscord.VoiceRoomEmbed(ddiscord.VoiceRoom{Owner: name}), voiceRoomButtons()))
	if _, err := h.channels.MoveMember(ctx, discordoutgress.MemberMoveRequest{GuildID: ev.GuildID, UserID: ev.UserID, ChannelID: reply.ChannelID}); err != nil {
		h.log.Warn("voice clone move failed", zap.Error(err))
	}
}

func voiceRoomButtons() []ddiscord.ButtonSpec {
	return []ddiscord.ButtonSpec{
		{Style: discordapi.ButtonDanger, Label: "Lock", CustomID: discordapi.CustomVoiceLock},
		{Style: discordapi.ButtonSuccess, Label: "Unlock", CustomID: discordapi.CustomVoiceUnlock},
	}
}

func (h voiceModule) deleteEmptyClone(ctx context.Context, channelID string) {
	if channelID == "" {
		return
	}
	cl, ok := h.store.Clone(ctx, discordstore.Channel{ID: channelID})
	if !ok {
		return
	}
	_ = h.store.ForgetClone(ctx, cl)
	if _, err := h.channels.DeleteChannel(ctx, discordoutgress.ChannelDeleteRequest{ChannelID: channelID}); err != nil {
		h.log.Warn("empty voice clone delete failed", zap.Error(err))
	}
}

type voiceInvocation struct {
	Module *module.Context
	In     decode.InteractionEvent
	Emit   module.Emit
}

func (h voiceModule) slash(ctx context.Context, c *module.Context, emit module.Emit) error {
	in, err := decode.Decode[decode.InteractionEvent](c.Event.Raw)
	if err != nil {
		return err
	}
	return h.command(ctx, voiceInvocation{Module: c, In: in, Emit: emit}, decode.FirstSub(in.Data.Options))
}

func (h voiceModule) lockButton(ctx context.Context, c *module.Context, emit module.Emit) error {
	in, err := decode.Decode[decode.InteractionEvent](c.Event.Raw)
	if err != nil {
		return err
	}
	return h.command(ctx, voiceInvocation{Module: c, In: in, Emit: emit}, decode.InteractionOption{Name: "lock"})
}

func (h voiceModule) unlockButton(ctx context.Context, c *module.Context, emit module.Emit) error {
	in, err := decode.Decode[decode.InteractionEvent](c.Event.Raw)
	if err != nil {
		return err
	}
	return h.command(ctx, voiceInvocation{Module: c, In: in, Emit: emit}, decode.InteractionOption{Name: "unlock"})
}

func (h voiceModule) command(ctx context.Context, v voiceInvocation, sub decode.InteractionOption) error {
	cl, ok := h.store.Clone(ctx, discordstore.Channel{ID: v.In.ChannelID})
	if !ok {
		v.Emit(cmd.Followup(cmd.GuildTarget(v.Module.Config.GuildID), cmd.Token(v.In.Token), "You can only do that in a temporary voice channel.", true))
		return nil
	}
	if !ownsVoice(cl, v.In, v.Module.Config) {
		v.Emit(cmd.Followup(cmd.GuildTarget(v.Module.Config.GuildID), cmd.Token(v.In.Token), "Only the channel owner can do that.", true))
		return nil
	}
	return h.apply(ctx, v, cl, sub)
}

func ownsVoice(cl discordstore.Clone, in decode.InteractionEvent, cfg ddiscord.Config) bool {
	if cl.OwnerID == in.Member.User.ID {
		return true
	}
	return isStaffOrMod(cfg, in)
}

func (h voiceModule) apply(ctx context.Context, v voiceInvocation, cl discordstore.Clone, sub decode.InteractionOption) error {
	switch sub.Name {
	case "name":
		return h.rename(ctx, v, cl, sub)
	case "limit":
		return h.limit(ctx, v, cl, sub)
	case "lock":
		return h.lock(ctx, v, cl, true)
	case "unlock":
		return h.lock(ctx, v, cl, false)
	default:
		v.Emit(cmd.Followup(cmd.GuildTarget(v.Module.Config.GuildID), cmd.Token(v.In.Token), "Unknown voice command.", true))
		return nil
	}
}

func (h voiceModule) rename(ctx context.Context, v voiceInvocation, cl discordstore.Clone, sub decode.InteractionOption) error {
	name := decode.OptionString(sub, "name")
	if name == "" {
		v.Emit(cmd.Followup(cmd.GuildTarget(v.Module.Config.GuildID), cmd.Token(v.In.Token), "Give the channel a name.", true))
		return nil
	}
	reply, err := h.channels.ModifyChannel(ctx, discordoutgress.ChannelModifyRequest{ChannelID: cl.ChannelID, Name: name})
	if rpcFailed(err, reply.Error) {
		h.log.Warn("voice rename failed", zap.Error(err), zap.String("outgress_error", reply.Error))
	}
	v.Emit(cmd.Followup(cmd.GuildTarget(v.Module.Config.GuildID), cmd.Token(v.In.Token), "Renamed.", true))
	return nil
}

func (h voiceModule) limit(ctx context.Context, v voiceInvocation, cl discordstore.Clone, sub decode.InteractionOption) error {
	n := decode.OptionInt(sub, "count")
	reply, err := h.channels.ModifyChannel(ctx, discordoutgress.ChannelModifyRequest{ChannelID: cl.ChannelID, UserLimit: n})
	if rpcFailed(err, reply.Error) {
		h.log.Warn("voice limit failed", zap.Error(err), zap.String("outgress_error", reply.Error))
	}
	v.Emit(cmd.Followup(cmd.GuildTarget(v.Module.Config.GuildID), cmd.Token(v.In.Token), "User limit set to "+strconv.Itoa(n)+".", true))
	return nil
}

func (h voiceModule) lock(ctx context.Context, v voiceInvocation, cl discordstore.Clone, lock bool) error {
	overwrites := []discordapi.PermissionOverwrite{
		decode.OverwriteAllow(decode.OverwriteSpec{TargetID: cl.OwnerID, Kind: 1, Bits: decode.PermView | decode.PermConnect}),
	}
	if lock {
		overwrites = append(overwrites, decode.OverwriteDeny(decode.OverwriteSpec{TargetID: cl.GuildID, Kind: 0, Bits: decode.PermConnect}))
	}
	reply, err := h.channels.ModifyChannel(ctx, discordoutgress.ChannelModifyRequest{ChannelID: cl.ChannelID, Overwrites: overwrites})
	if rpcFailed(err, reply.Error) {
		h.log.Warn("voice lock failed", zap.Error(err), zap.String("outgress_error", reply.Error))
	}
	msg := "Unlocked."
	if lock {
		msg = "Locked."
	}
	v.Emit(cmd.Followup(cmd.GuildTarget(v.Module.Config.GuildID), cmd.Token(v.In.Token), msg, true))
	return nil
}
