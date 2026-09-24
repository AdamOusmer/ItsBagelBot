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
	RemoveMemberRoleWithReason(ctx context.Context, r discapi.MemberRole, reason string) error
	InteractionFollowup(ctx context.Context, f discapi.Followup) error
	GetGuildMember(ctx context.Context, m discapi.GuildMember) (discapi.GuildMemberInfo, error)
	ListGuildRoles(ctx context.Context, guild discapi.Guild) ([]discapi.Snowflake, error)
	ListGuildChannelsFull(ctx context.Context, guild discapi.Guild) ([]discapi.ChannelInfo, error)
	GetGuild(ctx context.Context, guild discapi.Guild) (discapi.Snowflake, error)
	ModifyGuild(ctx context.Context, patch discapi.GuildPatch) error
	SetChannelOverwrite(ctx context.Context, o discapi.ChannelOverwrite) error
}

type Handlers struct {
	Rest          rest
	ApplicationID string
	Reauth        reauthStore
	Lockdown      lockdownStore
	Log           *zap.Logger
}

type lockdownStore interface {
	PutLockdown(ctx context.Context, guildID kv.GuildID, state kv.LockdownState) error
	GetLockdown(ctx context.Context, guildID kv.GuildID) (kv.LockdownState, bool)
	DeleteLockdown(ctx context.Context, guildID kv.GuildID) error
}

type reauthStore interface {
	MarkNeedsReauth(ctx context.Context, guildID kv.GuildID) error
	ClearNeedsReauth(ctx context.Context, guildID kv.GuildID) error
}

type commandHandler func(*Handlers, context.Context, ddiscord.Command) error

var dispatchTable = map[string]commandHandler{
	ddiscord.TypeDeleteMessage:       (*Handlers).deleteMessage,
	ddiscord.TypeBanMember:           banMember,
	ddiscord.TypeKickMember:          kickMember,
	ddiscord.TypeTimeoutMember:       (*Handlers).timeoutMember,
	ddiscord.TypeStripRoles:          stripRoles,
	ddiscord.TypeLockdown:            lockdown,
	ddiscord.TypeUnlock:              unlock,
	ddiscord.TypePostChat:            (*Handlers).postChat,
	ddiscord.TypePostEmbed:           (*Handlers).postEmbed,
	ddiscord.TypePostPanel:           (*Handlers).postPanel,
	ddiscord.TypeEditMessage:         (*Handlers).editMessage,
	ddiscord.TypeInteractionFollowup: (*Handlers).followup,
	ddiscord.TypeAddRole:             addRole,
	ddiscord.TypeRemoveRole:          removeRole,
	ddiscord.TypeSetGuildIdentity:    (*Handlers).setGuildIdentity,
}

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
