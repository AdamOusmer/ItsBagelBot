// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package commands

import (
	"context"
	"strconv"

	discapi "ItsBagelBot/internal/discordapi"
	ddiscord "ItsBagelBot/internal/domain/discord"
	"ItsBagelBot/pkg/codec"
)

// permSendMessages is Discord's SEND_MESSAGES bit. Overwrite allow/deny are
// decimal STRINGS because the bitfield exceeds 53 bits and would lose
// precision as a JSON number (same reason RoleCreate.Permissions is one).
const permSendMessages int64 = 1 << 11

// stripRoles removes every role Bagel is allowed to remove from a member.
//
// The roles are read here rather than carried on the command: see
// cmd.StripRoles. Managed roles (a bot's own, a booster role) are skipped
// because Discord refuses to remove them, and the @everyone role is skipped
// because it is not a grantable role at all -- its id is the guild id and it
// never appears in a member's role list.
//
// Every removal is attempted even after one fails, and the first error is
// returned so the lane redelivers: a partial strip leaves an attacker
// holding whichever role happened to be later in the list, and each removal
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
	managed := managedRoles(roles)
	var firstErr error
	for _, roleID := range member.Roles {
		if managed[roleID] || roleID == c.GuildID {
			continue
		}
		err := h.Rest.RemoveMemberRole(ctx, discapi.MemberRole{GuildID: c.GuildID, UserID: c.UserID, RoleID: roleID})
		if err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func managedRoles(roles []discapi.Snowflake) map[string]bool {
	out := make(map[string]bool, len(roles))
	for _, r := range roles {
		if r.Managed {
			out[r.ID] = true
		}
	}
	return out
}

// lockdown raises the guild's verification level to the highest tier and
// denies @everyone SEND in the text channels under the named categories.
//
// The verification bump goes first, and its failure is returned before any
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
	level := discapi.GuildVerificationHighest
	guild := discapi.Guild{ID: c.GuildID}
	if err := h.Rest.ModifyGuild(ctx, discapi.GuildPatch{Guild: guild, VerificationLevel: &level}); err != nil {
		return err
	}
	if len(p.CategoryIDs) == 0 {
		return nil
	}
	channels, err := h.Rest.ListGuildChannelsFull(ctx, guild)
	if err != nil {
		return err
	}
	return h.muteChannels(ctx, channels, lockdownScope{
		Everyone:   everyoneRole(p.EveryoneRoleID, c.GuildID),
		Categories: idSet(p.CategoryIDs),
	})
}

// lockdownScope is which channels to mute and which role to mute.
type lockdownScope struct {
	Everyone   string
	Categories map[string]bool
}

func (h *Handlers) muteChannels(ctx context.Context, channels []discapi.ChannelInfo, scope lockdownScope) error {
	var firstErr error
	for _, ch := range channels {
		if ch.Type != ddiscord.ChannelText || !scope.Categories[ch.ParentID] {
			continue
		}
		o := discapi.ChannelOverwrite{ChannelID: ch.ID, Overwrite: mutedOverwrite(ch, scope.Everyone)}
		if err := h.Rest.SetChannelOverwrite(ctx, o); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// mutedOverwrite adds SEND to the channel's existing @everyone deny and
// clears it from the allow. Discord's overwrite write REPLACES the named
// overwrite, so the current bits have to be carried forward: writing a bare
// deny would drop, for example, an existing VIEW allow and hide the channel.
func mutedOverwrite(ch discapi.ChannelInfo, everyone string) discapi.PermissionOverwrite {
	current := discapi.PermissionOverwrite{ID: everyone, Type: 0, Allow: "0", Deny: "0"}
	for _, o := range ch.PermissionOverwrites {
		if o.ID == everyone {
			current = o
			break
		}
	}
	return discapi.PermissionOverwrite{
		ID:    everyone,
		Type:  0,
		Allow: bits(parseBits(current.Allow) &^ permSendMessages),
		Deny:  bits(parseBits(current.Deny) | permSendMessages),
	}
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

// parseBits reads a decimal permission string. An unparseable value reads as
// zero, which for a deny means "we only add our own bit" -- the safe
// direction, since the alternative is refusing to lock down at all.
func parseBits(s string) int64 {
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0
	}
	return n
}

func bits(n int64) string { return strconv.FormatInt(n, 10) }
