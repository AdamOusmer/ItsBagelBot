// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package commands

import (
	"context"
	"fmt"

	"ItsBagelBot/app/discord/outgress/internal/identity"
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
	Log           *zap.Logger
}

// Dispatch runs the REST call for one Command. Called by Consumer.Handle.
func (h *Handlers) Dispatch(ctx context.Context, c ddiscord.Command) error {
	switch c.Type {
	case ddiscord.TypeDeleteMessage:
		return h.deleteMessage(ctx, c)
	case ddiscord.TypeBanMember:
		return h.Rest.BanMember(ctx, discapi.GuildMember{GuildID: c.GuildID, UserID: c.UserID})
	case ddiscord.TypeKickMember:
		return h.Rest.KickMember(ctx, discapi.GuildMember{GuildID: c.GuildID, UserID: c.UserID})
	case ddiscord.TypeTimeoutMember:
		return h.timeoutMember(ctx, c)
	case ddiscord.TypeStripRoles, ddiscord.TypeLockdown:
		h.Log.Warn("discord command type not yet implemented", zap.String("type", c.Type))
		return nil
	case ddiscord.TypePostChat:
		return h.postChat(ctx, c)
	case ddiscord.TypePostEmbed:
		return h.postEmbed(ctx, c)
	case ddiscord.TypePostPanel:
		return h.postPanel(ctx, c)
	case ddiscord.TypeEditMessage:
		return h.editMessage(ctx, c)
	case ddiscord.TypeInteractionFollowup:
		return h.followup(ctx, c)
	case ddiscord.TypeAddRole:
		return h.role(ctx, c, h.Rest.AddMemberRole)
	case ddiscord.TypeRemoveRole:
		return h.role(ctx, c, h.Rest.RemoveMemberRole)
	case ddiscord.TypeSetGuildIdentity:
		return h.setGuildIdentity(ctx, c)
	default:
		return fmt.Errorf("discord outgress: unknown command type %q", c.Type)
	}
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
	return h.Rest.ModifyCurrentMember(ctx, m)
}
