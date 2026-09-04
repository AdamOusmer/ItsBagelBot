// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package commands

import (
	"context"
	"errors"
	"fmt"

	"ItsBagelBot/app/discord/outgress/internal/identity"
	"ItsBagelBot/app/discord/outgress/internal/kv"
	discapi "ItsBagelBot/internal/discordapi"
	ddiscord "ItsBagelBot/internal/domain/discord"
	"ItsBagelBot/pkg/codec"

	"go.uber.org/zap"
)

// rest is the REST slice every Command Type below dispatches to.
type rest interface {
	SendChat(ctx context.Context, post discapi.ChatPost) error
	SendEmbed(ctx context.Context, post discapi.EmbedPost) (discapi.Message, error)
	SendPanel(ctx context.Context, post discapi.EmbedPost, buttons []discapi.Button) (discapi.Message, error)
	EditMessage(ctx context.Context, m discapi.Message, patch discapi.MessagePatch) error
	DeleteMessage(ctx context.Context, m discapi.Message) error
	TimeoutMember(ctx context.Context, t discapi.MemberTimeout) error
	KickMember(ctx context.Context, m discapi.GuildMember) error
	BanMember(ctx context.Context, m discapi.GuildMember) error
	AddMemberRole(ctx context.Context, r discapi.MemberRole) error
	ModifyCurrentMember(ctx context.Context, m discapi.CurrentMember) error
	RemoveMemberRole(ctx context.Context, r discapi.MemberRole) error
	InteractionFollowup(ctx context.Context, f discapi.Followup) error
}

// Handlers dispatches one Command onto its REST call. ApplicationID is
// learned once at boot (see ../bootstrap) and is what addresses the
// interaction-followup webhook; a Command never carries it because it is
// this process's own identity, not something engine decides per event.
type Handlers struct {
	Rest          rest
	ApplicationID string
	// Reauth records guilds whose bot role predates CHANGE_NICKNAME, so the
	// dashboard can prompt that streamer to re-authorize. Nil disables the
	// bookkeeping without changing what is sent to Discord.
	Reauth reauthStore
	Log    *zap.Logger
}

// reauthStore is the slice of kv.ReauthStore these handlers need.
type reauthStore interface {
	MarkNeedsReauth(ctx context.Context, guildID kv.GuildID) error
	ClearNeedsReauth(ctx context.Context, guildID kv.GuildID) error
}

// commandHandler is the shape every dispatchTable entry has: an unbound
// method (or a small closure standing in for one) taking the Handlers that
// own the REST call along with the usual ctx/Command pair. Using a method
// expression like (*Handlers).deleteMessage below lets the table hold the
// existing per-type methods directly, with no signature changes to them.
type commandHandler func(*Handlers, context.Context, ddiscord.Command) error

// dispatchTable is the Type -> handler map Dispatch looks up. It replaces
// what used to be a long type switch: the precedent for this shape is
// app/discord/engine/module/builder.go and app/outgress/internal/action's
// Registry, both of which trade a switch for a map validated once (there, at
// Build; here, simply by being a Go map literal the compiler already checks
// every entry of). Building it as a package-level var rather than per-call
// means Dispatch itself is one lookup, not a re-walk of every case.
var dispatchTable = map[string]commandHandler{
	ddiscord.TypeDeleteMessage:       (*Handlers).deleteMessage,
	ddiscord.TypeBanMember:           banMember,
	ddiscord.TypeKickMember:          kickMember,
	ddiscord.TypeTimeoutMember:       (*Handlers).timeoutMember,
	ddiscord.TypeStripRoles:          notImplemented,
	ddiscord.TypeLockdown:            notImplemented,
	ddiscord.TypePostChat:            (*Handlers).postChat,
	ddiscord.TypePostEmbed:           (*Handlers).postEmbed,
	ddiscord.TypePostPanel:           (*Handlers).postPanel,
	ddiscord.TypeEditMessage:         (*Handlers).editMessage,
	ddiscord.TypeInteractionFollowup: (*Handlers).followup,
	ddiscord.TypeAddRole:             addRole,
	ddiscord.TypeRemoveRole:          removeRole,
	ddiscord.TypeSetGuildIdentity:    (*Handlers).setGuildIdentity,
}

// Dispatch runs the REST call for one Command. Called by Consumer.Handle.
func (h *Handlers) Dispatch(ctx context.Context, c ddiscord.Command) error {
	fn, ok := dispatchTable[c.Type]
	if !ok {
		return fmt.Errorf("discord outgress: unknown command type %q", c.Type)
	}
	return fn(h, ctx, c)
}

func banMember(h *Handlers, ctx context.Context, c ddiscord.Command) error {
	return h.Rest.BanMember(ctx, discapi.GuildMember{GuildID: c.GuildID, UserID: c.UserID})
}

func kickMember(h *Handlers, ctx context.Context, c ddiscord.Command) error {
	return h.Rest.KickMember(ctx, discapi.GuildMember{GuildID: c.GuildID, UserID: c.UserID})
}

// notImplemented stands in for TypeStripRoles/TypeLockdown until their REST
// calls exist; it warns rather than erroring so an undelivered lockdown
// never nacks its lane forever.
func notImplemented(h *Handlers, _ context.Context, c ddiscord.Command) error {
	h.Log.Warn("discord command type not yet implemented", zap.String("type", c.Type))
	return nil
}

func addRole(h *Handlers, ctx context.Context, c ddiscord.Command) error {
	return h.role(ctx, c, h.Rest.AddMemberRole)
}

func removeRole(h *Handlers, ctx context.Context, c ddiscord.Command) error {
	return h.role(ctx, c, h.Rest.RemoveMemberRole)
}

func (h *Handlers) deleteMessage(ctx context.Context, c ddiscord.Command) error {
	var p ddiscord.DeletePayload
	if err := codec.Unmarshal(c.Payload, &p); err != nil {
		return err
	}
	return h.Rest.DeleteMessage(ctx, discapi.Message{ChannelID: c.ChannelID, ID: p.MessageID})
}

func (h *Handlers) timeoutMember(ctx context.Context, c ddiscord.Command) error {
	var p ddiscord.TimeoutPayload
	if err := codec.Unmarshal(c.Payload, &p); err != nil {
		return err
	}
	return h.Rest.TimeoutMember(ctx, discapi.MemberTimeout{
		GuildID: c.GuildID, UserID: c.UserID, UntilISO: p.UntilISO, Reason: c.Reason,
	})
}

func (h *Handlers) postChat(ctx context.Context, c ddiscord.Command) error {
	var p ddiscord.ChatPayload
	if err := codec.Unmarshal(c.Payload, &p); err != nil {
		return err
	}
	return h.Rest.SendChat(ctx, discapi.ChatPost{ChannelID: c.ChannelID, Content: p.Content})
}

func (h *Handlers) postEmbed(ctx context.Context, c ddiscord.Command) error {
	var p ddiscord.EmbedPayload
	if err := codec.Unmarshal(c.Payload, &p); err != nil {
		return err
	}
	_, err := h.Rest.SendEmbed(ctx, discapi.EmbedPost{ChannelID: c.ChannelID, Content: p.Content, Embed: p.Embed})
	return err
}

func (h *Handlers) postPanel(ctx context.Context, c ddiscord.Command) error {
	var p ddiscord.EmbedPayload
	if err := codec.Unmarshal(c.Payload, &p); err != nil {
		return err
	}
	_, err := h.Rest.SendPanel(ctx, discapi.EmbedPost{ChannelID: c.ChannelID, Content: p.Content, Embed: p.Embed}, toButtons(p.Buttons))
	return err
}

func (h *Handlers) editMessage(ctx context.Context, c ddiscord.Command) error {
	var p ddiscord.EditPayload
	if err := codec.Unmarshal(c.Payload, &p); err != nil {
		return err
	}
	return h.Rest.EditMessage(ctx, discapi.Message{ChannelID: c.ChannelID, ID: p.MessageID},
		discapi.MessagePatch{Content: p.Content, Embeds: p.Embeds})
}

func (h *Handlers) followup(ctx context.Context, c ddiscord.Command) error {
	var p ddiscord.FollowupPayload
	if err := codec.Unmarshal(c.Payload, &p); err != nil {
		return err
	}
	f := discapi.Followup{
		ApplicationID: h.ApplicationID, Token: p.InteractionToken,
		Content: p.Content, Buttons: toButtons(p.Buttons), Ephemeral: p.Ephemeral,
	}
	if p.Embed != nil {
		f.Embeds = []ddiscord.Embed{*p.Embed}
	}
	return h.Rest.InteractionFollowup(ctx, f)
}

func (h *Handlers) role(ctx context.Context, c ddiscord.Command, act func(context.Context, discapi.MemberRole) error) error {
	var p ddiscord.RolePayload
	if err := codec.Unmarshal(c.Payload, &p); err != nil {
		return err
	}
	return act(ctx, discapi.MemberRole{GuildID: c.GuildID, UserID: c.UserID, RoleID: p.RoleID})
}

func toButtons(specs []ddiscord.ButtonSpec) []discapi.Button {
	if len(specs) == 0 {
		return nil
	}
	out := make([]discapi.Button, 0, len(specs))
	for _, s := range specs {
		out = append(out, discapi.Button{Style: s.Style, Label: s.Label, CustomID: s.CustomID})
	}
	return out
}

// setGuildIdentity applies the bot's per-guild appearance. The command
// carries only a premium flag; the avatar bytes live here as an embedded
// asset (see internal/identity) rather than on the lane, because the same
// ~86 KB image would otherwise be serialized once per guild on every
// reconnect.
//
// A downgrade sends explicit nulls rather than skipping the fields: omitting
// them means "leave unchanged" to Discord, which would strand a premium
// nickname on a guild whose streamer stopped paying.
func (h *Handlers) setGuildIdentity(ctx context.Context, c ddiscord.Command) error {
	var p ddiscord.IdentityPayload
	if err := codec.Unmarshal(c.Payload, &p); err != nil {
		return err
	}
	m := discapi.CurrentMember{GuildID: c.GuildID}
	if nick, ok := p.Identity.Nick(); ok {
		m.Nick = &nick
	}
	if p.Identity.Premium {
		uri := identity.PremiumAvatarDataURI()
		m.AvatarDataURI = &uri
	}
	err := h.Rest.ModifyCurrentMember(ctx, m)
	if err == nil {
		h.clearReauth(ctx, kv.GuildID(c.GuildID))
		return nil
	}
	if m.Nick == nil || !errors.Is(err, discapi.ErrForbidden) {
		return err
	}
	// Discord refuses the WHOLE call when the nick is present and
	// CHANGE_NICKNAME is missing, avatar included. That permission is frozen
	// into the bot's role at install, so a guild that predates it will refuse
	// forever and retrying is pointless -- record it for the dashboard to
	// prompt a re-authorization, then retry without the nick so the premium
	// avatar still lands. Half the badge beats none of it.
	h.markReauth(ctx, kv.GuildID(c.GuildID))
	m.Nick = nil
	return h.Rest.ModifyCurrentMember(ctx, m)
}

func (h *Handlers) markReauth(ctx context.Context, guildID kv.GuildID) {
	h.log().Warn("discord rename refused; guild needs re-authorization for CHANGE_NICKNAME",
		zap.String("guild_id", string(guildID)))
	if h.Reauth == nil {
		return
	}
	if err := h.Reauth.MarkNeedsReauth(ctx, guildID); err != nil {
		h.log().Warn("failed to record discord reauth flag", zap.String("guild_id", string(guildID)), zap.Error(err))
	}
}

// clearReauth runs on every success, not just after a previous failure: a
// successful rename is the only proof the permission actually arrived, and
// the streamer who re-authorizes gets no other signal we could watch for.
func (h *Handlers) clearReauth(ctx context.Context, guildID kv.GuildID) {
	if h.Reauth == nil {
		return
	}
	if err := h.Reauth.ClearNeedsReauth(ctx, guildID); err != nil {
		h.log().Warn("failed to clear discord reauth flag", zap.String("guild_id", string(guildID)), zap.Error(err))
	}
}

func (h *Handlers) log() *zap.Logger {
	if h.Log != nil {
		return h.Log
	}
	return zap.NewNop()
}
