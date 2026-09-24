// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package commands

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"ItsBagelBot/app/discord/outgress/internal/kv"

	discapi "ItsBagelBot/internal/discordapi"
	ddiscord "ItsBagelBot/internal/domain/discord"
	"ItsBagelBot/pkg/codec"

	"go.uber.org/zap"
)

const permSendMessages int64 = 1 << 11

// A 403 must be skipped, not returned: returning it redelivers the command forever.
func stripRoles(h *Handlers, ctx context.Context, c ddiscord.Command) error {
	member, err := h.Rest.GetGuildMember(ctx, discapi.GuildMember{GuildID: c.GuildID, UserID: c.UserID})
	if err != nil {
		return err
	}
	roles, err := h.Rest.ListGuildRoles(ctx, discapi.Guild{ID: c.GuildID})
	if err != nil {
		return err
	}
	self, err := h.Rest.GetGuildMember(ctx, discapi.GuildMember{GuildID: c.GuildID, UserID: h.ApplicationID})
	if err != nil {
		return err
	}
	return h.removeEach(ctx, c, member.Roles, removable(roles, self.Roles))
}

func (h *Handlers) removeEach(ctx context.Context, c ddiscord.Command, ids []string, allowed map[string]bool) error {
	var firstErr error
	for _, roleID := range ids {
		if !allowed[roleID] || roleID == c.GuildID {
			continue
		}
		err := h.Rest.RemoveMemberRoleWithReason(ctx,
			discapi.MemberRole{GuildID: c.GuildID, UserID: c.UserID, RoleID: roleID}, c.Reason)
		switch {
		case err == nil:
		case errors.Is(err, discapi.ErrForbidden):
			h.log().Warn("strip roles: role refused, skipping",
				zap.String("guild_id", c.GuildID), zap.String("user_id", c.UserID),
				zap.String("role_id", roleID), zap.Error(err))
		case firstErr == nil:
			firstErr = err
		}
	}
	return firstErr
}

func removable(guildRoles []discapi.Snowflake, botRoles []string) map[string]bool {
	byID := make(map[string]discapi.Snowflake, len(guildRoles))
	for _, r := range guildRoles {
		byID[r.ID] = r
	}
	ceiling := highestPosition(byID, botRoles)
	out := make(map[string]bool, len(guildRoles))
	for _, r := range guildRoles {
		if !r.Managed && r.Position < ceiling {
			out[r.ID] = true
		}
	}
	return out
}

func highestPosition(byID map[string]discapi.Snowflake, ids []string) int {
	high := 0
	for _, id := range ids {
		if p := byID[id].Position; p > high {
			high = p
		}
	}
	return high
}

// Record the undo state before changing anything: the mute destroys the streamer's overwrites.
func lockdown(h *Handlers, ctx context.Context, c ddiscord.Command) error {
	var p ddiscord.LockdownPayload
	if len(c.Payload) > 0 {
		if err := codec.Unmarshal(c.Payload, &p); err != nil {
			return err
		}
	}
	plan, err := h.planLockdown(ctx, c, p)
	if err != nil {
		return err
	}
	h.rememberLockdown(ctx, c.GuildID, plan)
	level := discapi.GuildVerificationHighest
	guild := discapi.Guild{ID: c.GuildID}
	if err := h.Rest.ModifyGuild(ctx, discapi.GuildPatch{Guild: guild, VerificationLevel: &level}); err != nil {
		return err
	}
	return h.muteChannels(ctx, plan)
}

type lockdownPlan struct {
	Level    int
	Everyone string
	Channels []discapi.ChannelInfo
}

func (h *Handlers) planLockdown(ctx context.Context, c ddiscord.Command, p ddiscord.LockdownPayload) (lockdownPlan, error) {
	guild := discapi.Guild{ID: c.GuildID}
	before, err := h.Rest.GetGuild(ctx, guild)
	if err != nil {
		return lockdownPlan{}, err
	}
	plan := lockdownPlan{Level: before.VerificationLevel, Everyone: everyoneRole(p.EveryoneRoleID, c.GuildID)}
	if len(p.CategoryIDs) == 0 {
		return plan, nil
	}
	channels, err := h.Rest.ListGuildChannelsFull(ctx, guild)
	if err != nil {
		return lockdownPlan{}, err
	}
	plan.Channels = mutableChannels(channels, idSet(p.CategoryIDs))
	return plan, nil
}

func mutableChannels(channels []discapi.ChannelInfo, categories map[string]bool) []discapi.ChannelInfo {
	var out []discapi.ChannelInfo
	for _, ch := range channels {
		if mutableType(ch.Type) && categories[ch.ParentID] {
			out = append(out, ch)
		}
	}
	return out
}

func mutableType(t int) bool {
	switch t {
	case ddiscord.ChannelText, ddiscord.ChannelNews, ddiscord.ChannelForum:
		return true
	default:
		return false
	}
}

func (h *Handlers) rememberLockdown(ctx context.Context, guildID string, plan lockdownPlan) {
	if h.Lockdown == nil {
		h.log().Warn("lockdown: no undo store wired; this lockdown cannot be lifted by /unlock",
			zap.String("guild_id", guildID))
		return
	}
	if err := h.Lockdown.PutLockdown(ctx, kv.GuildID(guildID), plan.state()); err != nil {
		h.log().Error("lockdown: failed to record the undo state; locking down anyway",
			zap.String("guild_id", guildID), zap.Error(err))
	}
}

func (p lockdownPlan) state() kv.LockdownState {
	out := kv.LockdownState{VerificationLevel: p.Level, EveryoneRoleID: p.Everyone}
	for _, ch := range p.Channels {
		current := currentOverwrite(ch, p.Everyone)
		out.Channels = append(out.Channels, kv.LockdownChannel{
			ChannelID: ch.ID, Allow: current.Allow, Deny: current.Deny,
		})
	}
	return out
}

func (h *Handlers) muteChannels(ctx context.Context, plan lockdownPlan) error {
	var errs []error
	for _, ch := range plan.Channels {
		o, err := mutedOverwrite(ch, plan.Everyone)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if err := h.Rest.SetChannelOverwrite(ctx, discapi.ChannelOverwrite{ChannelID: ch.ID, Overwrite: o}); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func unlock(h *Handlers, ctx context.Context, c ddiscord.Command) error {
	state, ok := h.lockdownState(ctx, c.GuildID)
	if !ok {
		h.log().Warn("unlock: no lockdown state remembered for this guild; nothing to restore",
			zap.String("guild_id", c.GuildID))
		return nil
	}
	level := state.VerificationLevel
	guild := discapi.Guild{ID: c.GuildID}
	if err := h.Rest.ModifyGuild(ctx, discapi.GuildPatch{Guild: guild, VerificationLevel: &level}); err != nil {
		return err
	}
	if err := h.restoreOverwrites(ctx, state); err != nil {
		return err
	}
	return h.Lockdown.DeleteLockdown(ctx, kv.GuildID(c.GuildID))
}

func (h *Handlers) lockdownState(ctx context.Context, guildID string) (kv.LockdownState, bool) {
	if h.Lockdown == nil {
		return kv.LockdownState{}, false
	}
	return h.Lockdown.GetLockdown(ctx, kv.GuildID(guildID))
}

func (h *Handlers) restoreOverwrites(ctx context.Context, state kv.LockdownState) error {
	var errs []error
	for _, ch := range state.Channels {
		o := discapi.PermissionOverwrite{
			ID: state.EveryoneRoleID, Type: 0, Allow: ch.Allow, Deny: ch.Deny,
		}
		if err := h.Rest.SetChannelOverwrite(ctx, discapi.ChannelOverwrite{ChannelID: ch.ChannelID, Overwrite: o}); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func currentOverwrite(ch discapi.ChannelInfo, everyone string) discapi.PermissionOverwrite {
	for _, o := range ch.PermissionOverwrites {
		if o.ID == everyone {
			return o
		}
	}
	return discapi.PermissionOverwrite{ID: everyone, Type: 0, Allow: "0", Deny: "0"}
}

// Discord replaces the whole overwrite: a bare deny would drop a VIEW allow and hide the channel.
func mutedOverwrite(ch discapi.ChannelInfo, everyone string) (discapi.PermissionOverwrite, error) {
	current := currentOverwrite(ch, everyone)
	allow, err := parseBits(current.Allow)
	if err != nil {
		return discapi.PermissionOverwrite{}, fmt.Errorf("lockdown: channel %s allow bits: %w", ch.ID, err)
	}
	deny, err := parseBits(current.Deny)
	if err != nil {
		return discapi.PermissionOverwrite{}, fmt.Errorf("lockdown: channel %s deny bits: %w", ch.ID, err)
	}
	return discapi.PermissionOverwrite{
		ID:    everyone,
		Type:  0,
		Allow: bits(allow &^ permSendMessages),
		Deny:  bits(deny | permSendMessages),
	}, nil
}

func everyoneRole(id, guildID string) string {
	if id != "" {
		return id
	}
	return guildID
}

func idSet(ids []string) map[string]bool {
	out := make(map[string]bool, len(ids))
	for _, id := range ids {
		out[id] = true
	}
	return out
}

func parseBits(s string) (int64, error) {
	if s == "" {
		return 0, nil
	}
	return strconv.ParseInt(s, 10, 64)
}

func bits(n int64) string { return strconv.FormatInt(n, 10) }
