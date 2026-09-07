// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package commands

import (
	"context"
	"testing"

	discapi "ItsBagelBot/internal/discordapi"
	ddiscord "ItsBagelBot/internal/domain/discord"
)

func TestDispatchPostEmbed(t *testing.T) {
	rest := dispatchAll(t, ddiscord.Command{
		Type: ddiscord.TypePostEmbed, GuildID: "g1", ChannelID: "c1",
		Payload: marshalPayload(t, ddiscord.EmbedPayload{Embed: ddiscord.Embed{Title: "hi"}}),
	})
	if len(rest.embeds) != 1 || rest.embeds[0].Embed.Title != "hi" {
		t.Fatalf("embeds = %+v", rest.embeds)
	}
}

func TestDispatchPostPanelCarriesButtons(t *testing.T) {
	rest := dispatchAll(t, ddiscord.Command{
		Type: ddiscord.TypePostPanel, ChannelID: "c1",
		Payload: marshalPayload(t, ddiscord.EmbedPayload{
			Embed:   ddiscord.Embed{Title: "panel"},
			Buttons: []ddiscord.ButtonSpec{{Label: "Open", CustomID: "x"}},
		}),
	})
	if len(rest.panels) != 1 {
		t.Fatalf("panels = %+v", rest.panels)
	}
}

func TestDispatchEditMessage(t *testing.T) {
	rest := dispatchAll(t, ddiscord.Command{
		Type: ddiscord.TypeEditMessage, ChannelID: "c1",
		Payload: marshalPayload(t, ddiscord.EditPayload{MessageID: "m1", Content: "ended"}),
	})
	if len(rest.edited) != 1 || rest.edited[0].ID != "m1" {
		t.Fatalf("edited = %+v", rest.edited)
	}
}

func TestDispatchModerationTypes(t *testing.T) {
	rest := dispatchAll(t,
		ddiscord.Command{Type: ddiscord.TypeBanMember, GuildID: "g1", UserID: "u1"},
		ddiscord.Command{Type: ddiscord.TypeKickMember, GuildID: "g1", UserID: "u2"},
		ddiscord.Command{
			Type: ddiscord.TypeTimeoutMember, GuildID: "g1", UserID: "u3",
			Payload: marshalPayload(t, ddiscord.TimeoutPayload{UntilISO: "2026-01-01T00:00:00Z"}),
		},
		ddiscord.Command{
			Type: ddiscord.TypeDeleteMessage, ChannelID: "c1",
			Payload: marshalPayload(t, ddiscord.DeletePayload{MessageID: "m9"}),
		},
	)

	requireOneCall(t, rest.banned, func(m discapi.GuildMember) bool { return m.UserID == "u1" }, "banned")
	requireOneCall(t, rest.kicked, func(m discapi.GuildMember) bool { return m.UserID == "u2" }, "kicked")
	requireOneCall(t, rest.timeouts, func(m discapi.MemberTimeout) bool { return m.UntilISO != "" }, "timeouts")
	requireOneCall(t, rest.deleted, func(m discapi.Message) bool { return m.ID == "m9" }, "deleted")
}

func TestDispatchRoles(t *testing.T) {
	role := marshalPayload(t, ddiscord.RolePayload{RoleID: "r1"})
	rest := dispatchAll(t,
		ddiscord.Command{Type: ddiscord.TypeAddRole, GuildID: "g1", UserID: "u1", Payload: role},
		ddiscord.Command{Type: ddiscord.TypeRemoveRole, GuildID: "g1", UserID: "u1", Payload: role},
	)
	if len(rest.roleAdds) != 1 || rest.roleAdds[0].RoleID != "r1" {
		t.Fatalf("role adds = %+v", rest.roleAdds)
	}
	if len(rest.roleRems) != 1 {
		t.Fatalf("role removes = %+v", rest.roleRems)
	}
}

func TestDispatchFollowupUsesApplicationID(t *testing.T) {
	rest := &fakeRest{}
	h := &Handlers{Rest: rest, ApplicationID: "app-1"}
	cmd := ddiscord.Command{
		Type: ddiscord.TypeInteractionFollowup,
		Payload: marshalPayload(t, ddiscord.FollowupPayload{
			InteractionToken: "tok", Content: "done", Ephemeral: true,
		}),
	}
	dispatchOK(t, h, cmd)
	if len(rest.followups) != 1 {
		t.Fatalf("followups = %+v", rest.followups)
	}
	// Checked field by field, not as one f.ApplicationID != "app-1" ||
	// f.Token != "tok" || !f.Ephemeral condition: CodeScene's Complex
	// Conditional flags any single expression combining more than one
	// && / ||, and a three-field "something about this followup is wrong"
	// check is three separate claims, not one.
	f := rest.followups[0]
	if f.ApplicationID != "app-1" {
		t.Fatalf("followup ApplicationID = %q, want app-1 (followup %+v)", f.ApplicationID, f)
	}
	if f.Token != "tok" {
		t.Fatalf("followup Token = %q, want tok (followup %+v)", f.Token, f)
	}
	if !f.Ephemeral {
		t.Fatalf("followup Ephemeral = false, want true (followup %+v)", f)
	}
}

func TestDispatchUnknownTypeErrors(t *testing.T) {
	h := &Handlers{Rest: &fakeRest{}}
	if err := h.Dispatch(context.Background(), ddiscord.Command{Type: "bogus"}); err == nil {
		t.Fatal("expected an error for an unknown command type")
	}
}
