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

// permSendMessages is Discord's SEND_MESSAGES bit. Overwrite allow/deny are
// decimal STRINGS because the bitfield exceeds 53 bits and would lose
// precision as a JSON number (same reason RoleCreate.Permissions is one).
const permSendMessages int64 = 1 << 11

// stripRoles removes every role Bagel is allowed to remove from a member.
//
// The roles are read here rather than carried on the command: see
// cmd.StripRoles. Three classes are skipped, each for a different reason:
//
//   - MANAGED roles (a bot's own, a booster role) -- Discord refuses to
//     remove them from a member at all.
//   - roles at or ABOVE the bot's own highest role -- Discord's hierarchy
//     rule, and the reason the bot member is fetched: a strip that skips
//     only managed roles spends one call plus a retry on every admin role
//     it was never allowed to touch.
//   - @everyone, whose id is the guild id and which is not a grantable role.
//
// A 403 on an individual role is terminal for that role (the hierarchy or a
// permission changed under us) and is logged and skipped rather than
// returned: returning it nacks the command, and redelivery re-runs the whole
// strip forever against a role that can never be removed. Every other error
// is collected and the first returned, so the lane redelivers -- each removal
// is idempotent, so a redelivery costs calls but never correctness.
func stripRoles(h *Handlers, ctx context.Context, c ddiscord.Command) error {
	member, err := h.Rest.GetGuildMember(ctx, discapi.GuildMember{GuildID: c.GuildID, UserID: c.UserID})
	if err != nil {
		return err
	}
	roles, err := h.Rest.ListGuildRoles(ctx, discapi.Guild{ID: c.GuildID})
	if err != nil {
		return err
	}
	// The bot's own member. Its user id is its APPLICATION id: Discord
	// mints a bot user with the same snowflake as the application, and
	// outgress already learned that id once at boot (see ../bootstrap),
	// so this costs no extra call to /users/@me.
	self, err := h.Rest.GetGuildMember(ctx, discapi.GuildMember{GuildID: c.GuildID, UserID: h.ApplicationID})
	if err != nil {
		return err
	}
	return h.removeEach(ctx, c, member.Roles, removable(roles, self.Roles))
}

// removeEach revokes each role in ids that removable said may be revoked.
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

// removable is the set of guild role ids the bot may take off a member:
// unmanaged, and strictly below the bot's own highest role.
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

// highestPosition is the rank of the strongest role in ids. A bot holding no
// roles yields 0 (@everyone's position), which makes removable empty: the
// correct answer, since such a bot can remove nothing.
func highestPosition(byID map[string]discapi.Snowflake, ids []string) int {
	high := 0
	for _, id := range ids {
		if p := byID[id].Position; p > high {
			high = p
		}
	}
	return high
}

// lockdown raises the guild's verification level to the highest tier and
// denies @everyone SEND in the channels under the named categories.
//
// The order is: read what is about to be displaced, WRITE IT DOWN, then
// change anything. The write-down comes first because the lockdown destroys
// the information it needs to be undone -- once @everyone is denied SEND,
// nothing on Discord's side still says whether that deny was the streamer's
// own -- and a lockdown that cannot be lifted is a raid that never ends.
//
// The verification bump goes next, and its failure is returned before any
// channel is touched: it is the half that actually stops NEW accounts, and a
// caller retrying a lockdown that only muted channels would leave the door
// open. Requires MANAGE_GUILD (verification level) and MANAGE_ROLES /
// MANAGE_CHANNELS (overwrites); a bot invited before those were in the
// permission bitfield gets a 403 here, which surfaces as a nack rather than
// as the silent ACK this replaced.
func lockdown(h *Handlers, ctx context.Context, c ddiscord.Command) error {
	// An absent payload is a bare "lock the guild": raise verification and
	// touch no channel. Treating it as a decode error instead would nack a
	// command whose most important half needs no arguments at all.
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

// lockdownPlan is one lockdown's before-picture: the level to put back and
// the channels to mute, each still carrying the overwrites it had.
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

// mutableChannels picks the channels a mute means anything for: the three
// types where members POST. Voice (2) and stage (13) carry their own SPEAK
// bit rather than SEND, and a category (4) is not a channel members write
// in -- writing a SEND deny onto any of them spends a call to change
// nothing, which during a raid is a call the moderation lane needed.
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

// rememberLockdown records the undo state. A failure here is logged and NOT
// returned: the alternative is refusing to lock a guild down because Valkey
// is unreachable, and an unliftable lockdown is strictly better than a raid
// that was never stopped. Unlock reports the missing state itself.
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

// muteChannels writes the SEND deny onto every planned channel. Every channel
// is attempted and the failures are joined: one unwritable channel must not
// leave the rest of the guild open, and a rate limit on the fifth of twenty
// still has to nack so the lane redelivers the whole (idempotent) mute.
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

// unlock puts back what lockdown displaced: the verification level first (the
// half that matters), then each muted channel's @everyone overwrite, then the
// stored state.
//
// Nothing remembered means nothing to undo, which is TERMINAL rather than an
// error: the state expired, or the lockdown predates this store, and no
// amount of redelivery will conjure the old overwrites back.
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

// restoreOverwrites rewrites each remembered @everyone overwrite verbatim.
// Failures are joined and returned so the lane redelivers: the write is
// idempotent, and a channel left muted is a channel nobody can talk in.
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

// currentOverwrite is the channel's existing @everyone overwrite, or an
// explicit zero pair when it has none.
func currentOverwrite(ch discapi.ChannelInfo, everyone string) discapi.PermissionOverwrite {
	for _, o := range ch.PermissionOverwrites {
		if o.ID == everyone {
			return o
		}
	}
	return discapi.PermissionOverwrite{ID: everyone, Type: 0, Allow: "0", Deny: "0"}
}

// mutedOverwrite adds SEND to the channel's existing @everyone deny and
// clears it from the allow. Discord's overwrite write REPLACES the named
// overwrite, so the current bits have to be carried forward: writing a bare
// deny would drop, for example, an existing VIEW allow and hide the channel.
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

// everyoneRole falls back to the guild id, which is what Discord's
// @everyone role id always equals.
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

// parseBits reads a decimal permission string. An empty value is zero (the
// absent-overwrite default this file writes itself); anything else that does
// not parse FAILS its channel rather than reading as zero. Reading it as
// zero silently rewrote that channel's @everyone allow bits to nothing --
// dropping, for instance, a VIEW allow and hiding the channel from the
// server, permanently, as a side effect of a lockdown.
func parseBits(s string) (int64, error) {
	if s == "" {
		return 0, nil
	}
	return strconv.ParseInt(s, 10, 64)
}

func bits(n int64) string { return strconv.FormatInt(n, 10) }
