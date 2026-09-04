// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package commands

import (
	"context"
	"strings"
	"testing"

	discapi "ItsBagelBot/internal/discordapi"
	ddiscord "ItsBagelBot/internal/domain/discord"
	"ItsBagelBot/pkg/codec"
)

type fakeRest struct {
	identities []discapi.CurrentMember
	chats      []string
	embeds     []discapi.EmbedPost
	panels     []discapi.EmbedPost
	edited     []discapi.Message
	deleted    []discapi.Message
	timeouts   []discapi.MemberTimeout
	kicked     []discapi.GuildMember
	banned     []discapi.GuildMember
	roleAdds   []discapi.MemberRole
	roleRems   []discapi.MemberRole
	followups  []discapi.Followup
}

func (f *fakeRest) SendChat(_ context.Context, post discapi.ChatPost) error {
	f.chats = append(f.chats, post.Content)
	return nil
}
func (f *fakeRest) SendEmbed(_ context.Context, post discapi.EmbedPost) (discapi.Message, error) {
	f.embeds = append(f.embeds, post)
	return discapi.Message{ChannelID: post.ChannelID, ID: "m1"}, nil
}
func (f *fakeRest) SendPanel(_ context.Context, post discapi.EmbedPost, _ []discapi.Button) (discapi.Message, error) {
	f.panels = append(f.panels, post)
	return discapi.Message{ChannelID: post.ChannelID, ID: "p1"}, nil
}
func (f *fakeRest) EditMessage(_ context.Context, m discapi.Message, _ discapi.MessagePatch) error {
	f.edited = append(f.edited, m)
	return nil
}
func (f *fakeRest) DeleteMessage(_ context.Context, m discapi.Message) error {
	f.deleted = append(f.deleted, m)
	return nil
}
func (f *fakeRest) TimeoutMember(_ context.Context, t discapi.MemberTimeout) error {
	f.timeouts = append(f.timeouts, t)
	return nil
}
func (f *fakeRest) KickMember(_ context.Context, m discapi.GuildMember) error {
	f.kicked = append(f.kicked, m)
	return nil
}
func (f *fakeRest) BanMember(_ context.Context, m discapi.GuildMember) error {
	f.banned = append(f.banned, m)
	return nil
}
func (f *fakeRest) AddMemberRole(_ context.Context, r discapi.MemberRole) error {
	f.roleAdds = append(f.roleAdds, r)
	return nil
}
func (f *fakeRest) ModifyCurrentMember(_ context.Context, m discapi.CurrentMember) error {
	f.identities = append(f.identities, m)
	return nil
}
func (f *fakeRest) RemoveMemberRole(_ context.Context, r discapi.MemberRole) error {
	f.roleRems = append(f.roleRems, r)
	return nil
}
func (f *fakeRest) InteractionFollowup(_ context.Context, ff discapi.Followup) error {
	f.followups = append(f.followups, ff)
	return nil
}

func marshalPayload(t *testing.T, v any) []byte {
	t.Helper()
	raw, err := codec.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestDispatchPostEmbed(t *testing.T) {
	rest := &fakeRest{}
	h := &Handlers{Rest: rest}
	cmd := ddiscord.Command{
		Type: ddiscord.TypePostEmbed, GuildID: "g1", ChannelID: "c1",
		Payload: marshalPayload(t, ddiscord.EmbedPayload{Embed: ddiscord.Embed{Title: "hi"}}),
	}
	if err := h.Dispatch(context.Background(), cmd); err != nil {
		t.Fatal(err)
	}
	if len(rest.embeds) != 1 || rest.embeds[0].Embed.Title != "hi" {
		t.Fatalf("embeds = %+v", rest.embeds)
	}
}

func TestDispatchPostPanelCarriesButtons(t *testing.T) {
	rest := &fakeRest{}
	h := &Handlers{Rest: rest}
	cmd := ddiscord.Command{
		Type: ddiscord.TypePostPanel, ChannelID: "c1",
		Payload: marshalPayload(t, ddiscord.EmbedPayload{
			Embed:   ddiscord.Embed{Title: "panel"},
			Buttons: []ddiscord.ButtonSpec{{Label: "Open", CustomID: "x"}},
		}),
	}
	if err := h.Dispatch(context.Background(), cmd); err != nil {
		t.Fatal(err)
	}
	if len(rest.panels) != 1 {
		t.Fatalf("panels = %+v", rest.panels)
	}
}

func TestDispatchEditMessage(t *testing.T) {
	rest := &fakeRest{}
	h := &Handlers{Rest: rest}
	cmd := ddiscord.Command{
		Type: ddiscord.TypeEditMessage, ChannelID: "c1",
		Payload: marshalPayload(t, ddiscord.EditPayload{MessageID: "m1", Content: "ended"}),
	}
	if err := h.Dispatch(context.Background(), cmd); err != nil {
		t.Fatal(err)
	}
	if len(rest.edited) != 1 || rest.edited[0].ID != "m1" {
		t.Fatalf("edited = %+v", rest.edited)
	}
}

func TestDispatchModerationTypes(t *testing.T) {
	rest := &fakeRest{}
	h := &Handlers{Rest: rest}

	must := func(cmd ddiscord.Command) {
		t.Helper()
		if err := h.Dispatch(context.Background(), cmd); err != nil {
			t.Fatalf("dispatch %s: %v", cmd.Type, err)
		}
	}
	must(ddiscord.Command{Type: ddiscord.TypeBanMember, GuildID: "g1", UserID: "u1"})
	must(ddiscord.Command{Type: ddiscord.TypeKickMember, GuildID: "g1", UserID: "u2"})
	must(ddiscord.Command{
		Type: ddiscord.TypeTimeoutMember, GuildID: "g1", UserID: "u3",
		Payload: marshalPayload(t, ddiscord.TimeoutPayload{UntilISO: "2026-01-01T00:00:00Z"}),
	})
	must(ddiscord.Command{
		Type: ddiscord.TypeDeleteMessage, ChannelID: "c1",
		Payload: marshalPayload(t, ddiscord.DeletePayload{MessageID: "m9"}),
	})

	if len(rest.banned) != 1 || rest.banned[0].UserID != "u1" {
		t.Fatalf("banned = %+v", rest.banned)
	}
	if len(rest.kicked) != 1 || rest.kicked[0].UserID != "u2" {
		t.Fatalf("kicked = %+v", rest.kicked)
	}
	if len(rest.timeouts) != 1 || rest.timeouts[0].UntilISO == "" {
		t.Fatalf("timeouts = %+v", rest.timeouts)
	}
	if len(rest.deleted) != 1 || rest.deleted[0].ID != "m9" {
		t.Fatalf("deleted = %+v", rest.deleted)
	}
}

func TestDispatchRoles(t *testing.T) {
	rest := &fakeRest{}
	h := &Handlers{Rest: rest}
	add := ddiscord.Command{Type: ddiscord.TypeAddRole, GuildID: "g1", UserID: "u1", Payload: marshalPayload(t, ddiscord.RolePayload{RoleID: "r1"})}
	rem := ddiscord.Command{Type: ddiscord.TypeRemoveRole, GuildID: "g1", UserID: "u1", Payload: marshalPayload(t, ddiscord.RolePayload{RoleID: "r1"})}
	if err := h.Dispatch(context.Background(), add); err != nil {
		t.Fatal(err)
	}
	if err := h.Dispatch(context.Background(), rem); err != nil {
		t.Fatal(err)
	}
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
	if err := h.Dispatch(context.Background(), cmd); err != nil {
		t.Fatal(err)
	}
	if len(rest.followups) != 1 {
		t.Fatalf("followups = %+v", rest.followups)
	}
	f := rest.followups[0]
	if f.ApplicationID != "app-1" || f.Token != "tok" || !f.Ephemeral {
		t.Fatalf("followup = %+v", f)
	}
}

func TestDispatchUnknownTypeErrors(t *testing.T) {
	h := &Handlers{Rest: &fakeRest{}}
	if err := h.Dispatch(context.Background(), ddiscord.Command{Type: "bogus"}); err == nil {
		t.Fatal("expected an error for an unknown command type")
	}
}

func TestDispatchNotYetImplementedTypesNoop(t *testing.T) {
	h := &Handlers{Rest: &fakeRest{}, Log: testLogger()}
	if err := h.Dispatch(context.Background(), ddiscord.Command{Type: ddiscord.TypeStripRoles}); err != nil {
		t.Fatal(err)
	}
	if err := h.Dispatch(context.Background(), ddiscord.Command{Type: ddiscord.TypeLockdown}); err != nil {
		t.Fatal(err)
	}
}

// A premium apply must set BOTH halves in one call: the nickname needs
// CHANGE_NICKNAME, the avatar needs no permission, and sending them
// separately would leave a guild half-renamed whenever one of the two fails.
func TestSetGuildIdentityPremiumSendsNickAndAvatar(t *testing.T) {
	rest := &fakeRest{}
	h := &Handlers{Rest: rest}
	err := h.Dispatch(context.Background(), ddiscord.Command{
		Type: ddiscord.TypeSetGuildIdentity, GuildID: "g1",
		Payload: mustMarshal(t, ddiscord.IdentityPayload{Identity: ddiscord.GuildIdentity{Premium: true}}),
	})
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if len(rest.identities) != 1 {
		t.Fatalf("calls = %d, want 1", len(rest.identities))
	}
	got := rest.identities[0]
	if got.Nick == nil || *got.Nick != ddiscord.PremiumNick {
		t.Fatalf("nick = %v, want %q", got.Nick, ddiscord.PremiumNick)
	}
	if got.AvatarDataURI == nil || !strings.HasPrefix(*got.AvatarDataURI, "data:image/png;base64,") {
		t.Fatal("premium apply did not carry a png data URI")
	}
}

// A downgrade must send explicit nulls. Omitting the fields means "leave
// unchanged" to Discord, which would strand the premium nickname on a guild
// whose streamer stopped paying.
func TestSetGuildIdentityDefaultClearsBothOverrides(t *testing.T) {
	rest := &fakeRest{}
	h := &Handlers{Rest: rest}
	err := h.Dispatch(context.Background(), ddiscord.Command{
		Type: ddiscord.TypeSetGuildIdentity, GuildID: "g1",
		Payload: mustMarshal(t, ddiscord.IdentityPayload{Identity: ddiscord.GuildIdentity{Premium: false}}),
	})
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	got := rest.identities[0]
	if got.Nick != nil || got.AvatarDataURI != nil {
		t.Fatalf("downgrade did not clear both overrides: nick=%v avatar set=%v", got.Nick, got.AvatarDataURI != nil)
	}
}

func mustMarshal(t *testing.T, v any) []byte {
	t.Helper()
	raw, err := codec.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return raw
}
