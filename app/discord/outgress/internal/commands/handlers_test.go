// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package commands_test

import (
	"context"
	"testing"

	"ItsBagelBot/app/discord/outgress/internal/commands"
	"ItsBagelBot/app/discord/outgress/internal/identity"
	"ItsBagelBot/app/discord/outgress/internal/kv"
	discapi "ItsBagelBot/internal/discordapi"
	ddiscord "ItsBagelBot/internal/domain/discord"

	"github.com/stretchr/testify/require"
)

func TestDispatch(t *testing.T) {
	member := discapi.GuildMember{GuildID: "g1", UserID: "u1"}
	role := discapi.MemberRole{GuildID: "g1", UserID: "u1", RoleID: "r1"}
	cases := []struct {
		name    string
		kind    string
		payload []byte
		want    []call
	}{
		{"posts chat to the channel", ddiscord.TypePostChat, encode(t, ddiscord.ChatPayload{Content: "hi"}),
			[]call{{"SendChat", discapi.ChatPost{ChannelID: "c1", Content: "hi"}}}},
		{"posts an embed", ddiscord.TypePostEmbed, encode(t, ddiscord.EmbedPayload{Embed: ddiscord.Embed{Title: "hi"}}),
			[]call{{"SendEmbed", discapi.EmbedPost{ChannelID: "c1", Embed: ddiscord.Embed{Title: "hi"}}}}},
		{"posts a panel with its buttons", ddiscord.TypePostPanel, encode(t, ddiscord.EmbedPayload{
			Embed: ddiscord.Embed{Title: "panel"}, Buttons: []ddiscord.ButtonSpec{{Label: "Open", CustomID: "x"}},
		}), []call{{"SendPanel", panelPost{
			Post:    discapi.EmbedPost{ChannelID: "c1", Embed: ddiscord.Embed{Title: "panel"}},
			Buttons: []discapi.Button{{Label: "Open", CustomID: "x"}},
		}}}},
		{"edits a message", ddiscord.TypeEditMessage, encode(t, ddiscord.EditPayload{MessageID: "m1", Content: "ended"}),
			[]call{{"EditMessage", messageEdit{
				Message: discapi.Message{ChannelID: "c1", ID: "m1"}, Patch: discapi.MessagePatch{Content: "ended"},
			}}}},
		{"deletes a message", ddiscord.TypeDeleteMessage, encode(t, ddiscord.DeletePayload{MessageID: "m9"}),
			[]call{{"DeleteMessage", discapi.Message{ChannelID: "c1", ID: "m9"}}}},
		{"bans the member", ddiscord.TypeBanMember, nil, []call{{"BanMember", member}}},
		{"kicks the member", ddiscord.TypeKickMember, nil, []call{{"KickMember", member}}},
		{"times the member out with the audit reason", ddiscord.TypeTimeoutMember,
			encode(t, ddiscord.TimeoutPayload{UntilISO: "2026-01-01T00:00:00Z"}),
			[]call{{"TimeoutMember", discapi.MemberTimeout{GuildID: "g1", UserID: "u1", UntilISO: "2026-01-01T00:00:00Z", Reason: "spam"}}}},
		{"adds a role", ddiscord.TypeAddRole, encode(t, ddiscord.RolePayload{RoleID: "r1"}), []call{{"AddMemberRole", role}}},
		{"removes a role", ddiscord.TypeRemoveRole, encode(t, ddiscord.RolePayload{RoleID: "r1"}), []call{{"RemoveMemberRole", role}}},
		{"answers an interaction as the application", ddiscord.TypeInteractionFollowup, encode(t, ddiscord.FollowupPayload{
			InteractionToken: "tok", Content: "done", Ephemeral: true, Embed: &ddiscord.Embed{Title: "result"},
		}), []call{{"InteractionFollowup", discapi.Followup{
			ApplicationID: "app-1", Token: "tok", Content: "done", Ephemeral: true, Embeds: []ddiscord.Embed{{Title: "result"}},
		}}}},
		{"refuses an unknown command type", "bogus", nil, nil},
		{"refuses a payload it cannot decode", ddiscord.TypeTimeoutMember, []byte("{"), nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rest := &fakeRest{}
			h := &commands.Handlers{Rest: rest, ApplicationID: "app-1"}

			err := h.Dispatch(context.Background(), ddiscord.Command{
				Type: tc.kind, GuildID: "g1", ChannelID: "c1", UserID: "u1", Reason: "spam", Payload: tc.payload,
			})

			require.Equal(t, tc.want == nil, err != nil, "Dispatch error: %v", err)
			require.Equal(t, tc.want, rest.calls)
		})
	}
}

func TestChannelCommandsRejectForeignGuildBeforeREST(t *testing.T) {
	for _, kind := range []string{ddiscord.TypePostChat, ddiscord.TypePostEmbed, ddiscord.TypePostPanel, ddiscord.TypeEditMessage, ddiscord.TypeDeleteMessage} {
		t.Run(kind, func(t *testing.T) {
			rest := &fakeRest{channelGuilds: map[string]string{"c2": "g2"}}
			h := &commands.Handlers{Rest: rest}

			err := h.Dispatch(context.Background(), ddiscord.Command{Type: kind, GuildID: "g1", ChannelID: "c2"})

			require.ErrorIs(t, err, discapi.ErrForbidden)
			require.Empty(t, rest.calls, "foreign REST side effect")
		})
	}
}

func TestSetGuildIdentity(t *testing.T) {
	nick, avatar := ddiscord.PremiumNick, identity.PremiumAvatarDataURI()
	cases := []struct {
		name    string
		premium bool
		errs    []error
		want    []call
		wantErr error
		reauth  fakeReauth
	}{{
		name:    "premium sets the bagel nick and avatar and clears the reauth flag",
		premium: true,
		want:    []call{{"ModifyCurrentMember", discapi.CurrentMember{GuildID: "g1", Nick: &nick, AvatarDataURI: &avatar}}},
		reauth:  fakeReauth{Cleared: []kv.GuildID{"g1"}},
	}, {
		name:   "default clears both overrides",
		want:   []call{{"ModifyCurrentMember", discapi.CurrentMember{GuildID: "g1"}}},
		reauth: fakeReauth{Cleared: []kv.GuildID{"g1"}},
	}, {
		name:    "a refused rename falls back to the avatar alone and flags the guild for reauth",
		premium: true,
		errs:    []error{discapi.ErrForbidden},
		want:    []call{{"ModifyCurrentMember", discapi.CurrentMember{GuildID: "g1", AvatarDataURI: &avatar}}},
		reauth:  fakeReauth{Marked: []kv.GuildID{"g1"}},
	}, {
		name:    "a refusal with no nick to drop surfaces for redelivery",
		errs:    []error{discapi.ErrForbidden},
		wantErr: discapi.ErrForbidden,
	}, {
		name:    "a transient failure surfaces for redelivery",
		premium: true,
		errs:    []error{errBoom},
		wantErr: errBoom,
	}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rest := &fakeRest{errs: map[op][]error{"ModifyCurrentMember": tc.errs}}
			reauth := &fakeReauth{}
			h := &commands.Handlers{Rest: rest, Reauth: reauth}

			err := h.Dispatch(context.Background(), ddiscord.Command{
				Type: ddiscord.TypeSetGuildIdentity, GuildID: "g1",
				Payload: encode(t, ddiscord.IdentityPayload{Identity: ddiscord.GuildIdentity{Premium: tc.premium}}),
			})

			require.ErrorIs(t, err, tc.wantErr)
			require.Equal(t, tc.want, rest.calls)
			require.Equal(t, tc.reauth, *reauth)
		})
	}
}
