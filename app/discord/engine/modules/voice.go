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

// voiceClient is the full set of RPCs join-to-create needs: create/delete a
// clone, and move its owner into it. MoveMember has no Command type at all
// (see internal/domain/discord's Command doc, which lists none for voice
// state) because it is the one Discord REST call with no analogue on the
// Twitch outgress side to model the vocabulary after -- ModType() has no
// opinion on it either way, so it was never a candidate for the mod lane.
type voiceClient interface {
	channelClient
	ModifyChannel(ctx context.Context, req discordoutgress.ChannelModifyRequest) (discordoutgress.ChannelModifyReply, error)
	MoveMember(ctx context.Context, req discordoutgress.MemberMoveRequest) (discordoutgress.MemberMoveReply, error)
}

// Voice ports app/dingress/internal/community/voice.go: join-to-create voice
// clones and their owner-only /voice name|limit|lock|unlock controls.
//
// The in-memory occupancy tracker community used to decide when a clone
// empties is gone; discordstore.Store.UpdateVoiceOccupancy (Valkey-backed)
// replaces it, because engine -- unlike the old single-replica gateway
// process -- may run more than one replica, and "who is still in this
// channel" has to be visible across all of them.
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
		// See voiceClient's doc: this replaces community's exact
		// "== the bot's own gateway identity" check, which engine cannot
		// perform (it has no gateway session and never learns its own user
		// id). Filtering every bot's voice presence is equivalent here in
		// practice -- this bot never joins voice itself, so the two checks
		// only differ for a hypothetical second bot in the same channel,
		// which was never a join-to-create trigger either way.
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

// joinedVoiceHub reports whether channelID is a join-to-create trigger:
// voice is on, the member landed in a channel at all (an empty channel id
// means they left one instead of joining), and it is specifically the
// configured hub, not some other voice channel in the guild.
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

func (h voiceModule) slash(ctx context.Context, c *module.Context, emit module.Emit) error {
	in, err := decode.Decode[decode.InteractionEvent](c.Event.Raw)
	if err != nil {
		return err
	}
	return h.command(ctx, c, in, decode.FirstSub(in.Data.Options), emit)
}

func (h voiceModule) lockButton(ctx context.Context, c *module.Context, emit module.Emit) error {
	in, err := decode.Decode[decode.InteractionEvent](c.Event.Raw)
	if err != nil {
		return err
	}
	return h.command(ctx, c, in, decode.InteractionOption{Name: "lock"}, emit)
}

func (h voiceModule) unlockButton(ctx context.Context, c *module.Context, emit module.Emit) error {
	in, err := decode.Decode[decode.InteractionEvent](c.Event.Raw)
	if err != nil {
		return err
	}
	return h.command(ctx, c, in, decode.InteractionOption{Name: "unlock"}, emit)
}

func (h voiceModule) command(ctx context.Context, c *module.Context, in decode.InteractionEvent, sub decode.InteractionOption, emit module.Emit) error {
	cl, ok := h.store.Clone(ctx, discordstore.Channel{ID: in.ChannelID})
	if !ok {
		emit(cmd.Followup(cmd.GuildTarget(c.Config.GuildID), in.Token, "You can only do that in a temporary voice channel.", true))
		return nil
	}
	if !ownsVoice(cl, in) {
		emit(cmd.Followup(cmd.GuildTarget(c.Config.GuildID), in.Token, "Only the channel owner can do that.", true))
		return nil
	}
	return h.apply(ctx, c, in, cl, sub, emit)
}

func ownsVoice(cl discordstore.Clone, in decode.InteractionEvent) bool {
	if cl.OwnerID == in.Member.User.ID {
		return true
	}
	return decode.CanMod(in.Member.Permissions)
}

func (h voiceModule) apply(ctx context.Context, c *module.Context, in decode.InteractionEvent, cl discordstore.Clone, sub decode.InteractionOption, emit module.Emit) error {
	switch sub.Name {
	case "name":
		return h.rename(ctx, c, in, cl, sub, emit)
	case "limit":
		return h.limit(ctx, c, in, cl, sub, emit)
	case "lock":
		return h.lock(ctx, c, in, cl, true, emit)
	case "unlock":
		return h.lock(ctx, c, in, cl, false, emit)
	default:
		emit(cmd.Followup(cmd.GuildTarget(c.Config.GuildID), in.Token, "Unknown voice command.", true))
		return nil
	}
}

func (h voiceModule) rename(ctx context.Context, c *module.Context, in decode.InteractionEvent, cl discordstore.Clone, sub decode.InteractionOption, emit module.Emit) error {
	name := decode.OptionString(sub, "name")
	if name == "" {
		emit(cmd.Followup(cmd.GuildTarget(c.Config.GuildID), in.Token, "Give the channel a name.", true))
		return nil
	}
	reply, err := h.channels.ModifyChannel(ctx, discordoutgress.ChannelModifyRequest{ChannelID: cl.ChannelID, Name: name})
	if rpcFailed(err, reply.Error) {
		h.log.Warn("voice rename failed", zap.Error(err), zap.String("outgress_error", reply.Error))
	}
	emit(cmd.Followup(cmd.GuildTarget(c.Config.GuildID), in.Token, "Renamed.", true))
	return nil
}

func (h voiceModule) limit(ctx context.Context, c *module.Context, in decode.InteractionEvent, cl discordstore.Clone, sub decode.InteractionOption, emit module.Emit) error {
	n := decode.OptionInt(sub, "count")
	reply, err := h.channels.ModifyChannel(ctx, discordoutgress.ChannelModifyRequest{ChannelID: cl.ChannelID, UserLimit: n})
	if rpcFailed(err, reply.Error) {
		h.log.Warn("voice limit failed", zap.Error(err), zap.String("outgress_error", reply.Error))
	}
	emit(cmd.Followup(cmd.GuildTarget(c.Config.GuildID), in.Token, "User limit set to "+strconv.Itoa(n)+".", true))
	return nil
}

func (h voiceModule) lock(ctx context.Context, c *module.Context, in decode.InteractionEvent, cl discordstore.Clone, lock bool, emit module.Emit) error {
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
	emit(cmd.Followup(cmd.GuildTarget(c.Config.GuildID), in.Token, msg, true))
	return nil
}
