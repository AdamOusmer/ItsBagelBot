// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"sync"

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
	h := voiceModule{store: store, channels: channels, log: log, warned: &sync.Map{}}
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
	warned   *sync.Map
}

func (h voiceModule) onVoiceState(ctx context.Context, c *module.Context, emit module.Emit) error {
	ev, err := decode.Decode[decode.VoiceEvent](c.Event.Raw)
	if err != nil {
		return err
	}
	if ev.Member.User.Bot {
		return nil
	}
	move := h.store.UpdateVoiceOccupancy(ctx, discordstore.VoiceSeat{
		GuildID: ev.GuildID, UserID: ev.UserID, ChannelID: ev.ChannelID,
	})
	logVoiceMove(c, emit, ev.UserID, move)
	if move.LeftEmpty {
		h.deleteEmptyClone(ctx, move.From)
	}
	if enteredVoiceHub(c.Config, move) {
		h.enterHub(ctx, c, ev, emit)
	}
	return nil
}

func (h voiceModule) enterHub(ctx context.Context, c *module.Context, ev decode.VoiceEvent, emit module.Emit) {
	if !c.Config.VoiceCategorySet() {
		h.warnNoCategory(ev.GuildID)
		return
	}
	h.cloneAndMove(ctx, c, ev, emit)
}

func (h voiceModule) warnNoCategory(guildID string) {
	if _, seen := h.warned.LoadOrStore(guildID, struct{}{}); !seen {
		h.log.Warn("voice hub entered but no room category is configured; creating nothing", zap.String("guild_id", guildID))
	}
}

func enteredVoiceHub(cfg ddiscord.Config, move discordstore.VoiceMove) bool {
	return cfg.VoiceOn() && move.To != "" && move.To == cfg.VoiceHubID && move.From != move.To
}

type voiceRoom struct {
	GuildID string
	OwnerID string
	Locked  bool
}

func voiceOverwrites(cfg ddiscord.Config, room voiceRoom) []discordapi.PermissionOverwrite {
	out := []discordapi.PermissionOverwrite{
		decode.OverwriteAllow(decode.OverwriteSpec{TargetID: room.OwnerID, Kind: 1, Bits: decode.PermView | decode.PermConnect | decode.PermSend}),
	}
	var deny int64
	if room.Locked || cfg.VoicePrivacy() == ddiscord.VoicePrivacyLocked {
		deny |= decode.PermConnect
	}
	if cfg.VoicePrivacy() == ddiscord.VoicePrivacyHidden {
		deny |= decode.PermView
	}
	if deny != 0 {
		out = append(out, decode.OverwriteDeny(decode.OverwriteSpec{TargetID: room.GuildID, Kind: 0, Bits: deny}))
	}
	return out
}

func (h voiceModule) cloneAndMove(ctx context.Context, c *module.Context, ev decode.VoiceEvent, emit module.Emit) {
	if h.store.CloneCount(ctx, discordstore.Guild{ID: ev.GuildID}) >= ddiscord.VoiceCloneCap {
		return
	}
	owner := decode.DisplayName(decode.Display{User: ev.Member.User, Nick: ev.Member.Nick})
	reply, err := h.channels.CreateChannel(ctx, discordoutgress.ChannelCreateRequest{
		GuildID: ev.GuildID, Name: c.Config.VoiceName(owner), Type: ddiscord.ChannelVoice,
		ParentID: c.Config.VoiceCategoryID, UserLimit: c.Config.VoiceLimit(), Overwrites: voiceOverwrites(c.Config, voiceRoom{GuildID: ev.GuildID, OwnerID: ev.UserID}),
	})
	if rpcFailed(err, reply.Error) || reply.ChannelID == "" {
		h.log.Warn("voice clone create failed", zap.Error(err), zap.String("outgress_error", reply.Error))
		return
	}
	cl := discordstore.Clone{ChannelID: reply.ChannelID, GuildID: ev.GuildID, OwnerID: ev.UserID}
	if err := h.store.TrackClone(ctx, cl); err != nil {
		if errors.Is(err, discordstore.ErrCloneCapReached) {
			h.log.Info("voice clone cap reached; dropping the new room", zap.String("guild_id", ev.GuildID))
		} else {
			h.log.Warn("voice clone tracking failed", zap.Error(err))
		}
		h.deleteClone(ctx, cl)
		return
	}
	moved, err := h.channels.MoveMember(ctx, discordoutgress.MemberMoveRequest{GuildID: ev.GuildID, UserID: ev.UserID, ChannelID: cl.ChannelID})
	if rpcFailed(err, moved.Error) {
		h.log.Warn("voice clone move failed", zap.Error(err), zap.String("outgress_error", moved.Error))
		h.deleteClone(ctx, cl)
		return
	}
	emit(cmd.PostPanel(cmd.ChannelTarget(ev.GuildID, cl.ChannelID), "", ddiscord.VoiceRoomEmbed(ddiscord.VoiceRoom{Owner: owner}), voiceRoomButtons()))
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
	h.deleteClone(ctx, cl)
}

func (h voiceModule) deleteClone(ctx context.Context, cl discordstore.Clone) {
	reply, err := h.channels.DeleteChannel(ctx, discordoutgress.ChannelDeleteRequest{GuildID: cl.GuildID, ChannelID: cl.ChannelID})
	if rpcFailed(err, reply.Error) {
		h.log.Warn("voice clone delete failed", zap.Error(err), zap.String("outgress_error", reply.Error))
		return
	}
	if err := h.store.ForgetClone(ctx, cl); err != nil {
		h.log.Warn("voice clone forget failed", zap.Error(err))
	}
}

type voiceInvocation struct {
	Module *module.Context
	In     decode.InteractionEvent
	Emit   module.Emit
}

func (h voiceModule) slash(ctx context.Context, c *module.Context, emit module.Emit) error {
	return h.interact(ctx, c, emit, nil)
}

func (h voiceModule) lockButton(ctx context.Context, c *module.Context, emit module.Emit) error {
	return h.interact(ctx, c, emit, &decode.InteractionOption{Name: "lock"})
}

func (h voiceModule) unlockButton(ctx context.Context, c *module.Context, emit module.Emit) error {
	return h.interact(ctx, c, emit, &decode.InteractionOption{Name: "unlock"})
}

func (h voiceModule) interact(ctx context.Context, c *module.Context, emit module.Emit, fixed *decode.InteractionOption) error {
	in, err := decode.Decode[decode.InteractionEvent](c.Event.Raw)
	if err != nil {
		return err
	}
	sub := decode.FirstSub(in.Data.Options)
	if fixed != nil {
		sub = *fixed
	}
	return h.command(ctx, voiceInvocation{Module: c, In: in, Emit: emit}, sub)
}

func (h voiceModule) command(ctx context.Context, v voiceInvocation, sub decode.InteractionOption) error {
	cl, ok := h.store.Clone(ctx, discordstore.Channel{ID: v.In.ChannelID})
	if !ok {
		h.say(v, "You can only do that in a temporary voice channel.")
		return nil
	}
	if !ownsVoice(cl, v.In, v.Module.Config) {
		h.say(v, "Only the channel owner can do that.")
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
		h.say(v, "Unknown voice command.")
		return nil
	}
}

type voiceChange struct {
	Op      string
	Done    string
	Limited string
}

const genericLimited = "Discord is rate limiting that change. Try again shortly."

func (h voiceModule) say(v voiceInvocation, text string) {
	v.Emit(cmd.Followup(cmd.GuildTarget(v.Module.Config.GuildID), cmd.Token(v.In.Token), text, true))
}

func (h voiceModule) modify(ctx context.Context, v voiceInvocation, req discordoutgress.ChannelModifyRequest, change voiceChange) error {
	reply, err := h.channels.ModifyChannel(ctx, req)
	if !rpcFailed(err, reply.Error) {
		h.say(v, change.Done)
		return nil
	}
	h.log.Warn("voice "+change.Op+" failed", zap.Error(err), zap.String("outgress_error", reply.Error))
	h.say(v, voiceFailure(change, err, reply.Error))
	return nil
}

func voiceFailure(change voiceChange, err error, outgressErr string) string {
	if !strings.Contains(outgressErr, discordapi.ErrRateLimited.Error()) && !errors.Is(err, discordapi.ErrRateLimited) {
		return "Could not change the " + change.Op + " right now."
	}
	if change.Limited != "" {
		return change.Limited
	}
	return genericLimited
}

func (h voiceModule) rename(ctx context.Context, v voiceInvocation, cl discordstore.Clone, sub decode.InteractionOption) error {
	name := decode.OptionString(sub, "name")
	if name == "" {
		h.say(v, "Give the channel a name.")
		return nil
	}
	req := discordoutgress.ChannelModifyRequest{GuildID: cl.GuildID, ChannelID: cl.ChannelID, Name: name}
	return h.modify(ctx, v, req, voiceChange{
		Op: "name", Done: "Renamed.",
		Limited: "Discord only allows 2 renames per 10 minutes. Try again later.",
	})
}

func (h voiceModule) limit(ctx context.Context, v voiceInvocation, cl discordstore.Clone, sub decode.InteractionOption) error {
	n := decode.OptionInt(sub, "count")
	if n < 0 || n > ddiscord.VoiceUserLimitMax {
		h.say(v, "Pick a limit from 0 to "+strconv.Itoa(ddiscord.VoiceUserLimitMax)+".")
		return nil
	}
	done := "User limit set to " + strconv.Itoa(n) + "."
	if n == 0 {
		done = "User limit cleared."
	}
	req := discordoutgress.ChannelModifyRequest{GuildID: cl.GuildID, ChannelID: cl.ChannelID, UserLimit: &n}
	return h.modify(ctx, v, req, voiceChange{Op: "user limit", Done: done})
}

func (h voiceModule) lock(ctx context.Context, v voiceInvocation, cl discordstore.Clone, lock bool) error {
	done := "Unlocked."
	if lock {
		done = "Locked."
	}
	overwrites := voiceOverwrites(v.Module.Config, voiceRoom{GuildID: cl.GuildID, OwnerID: cl.OwnerID, Locked: lock})
	req := discordoutgress.ChannelModifyRequest{GuildID: cl.GuildID, ChannelID: cl.ChannelID, Overwrites: overwrites}
	return h.modify(ctx, v, req, voiceChange{Op: "lock", Done: done})
}
