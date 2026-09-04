// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package commands

import (
	"context"
	"errors"
	"strings"
	"testing"

	discapi "ItsBagelBot/internal/discordapi"
	ddiscord "ItsBagelBot/internal/domain/discord"
	"ItsBagelBot/pkg/codec"
)

type fakeRest struct {
	identities   []discapi.CurrentMember
	identityErrs []error
	chats        []string
	embeds       []discapi.EmbedPost
	panels       []discapi.EmbedPost
	edited       []discapi.Message
	deleted      []discapi.Message
	timeouts     []discapi.MemberTimeout
	kicked       []discapi.GuildMember
	banned       []discapi.GuildMember
	roleAdds     []discapi.MemberRole
	roleRems     []discapi.MemberRole
	followups    []discapi.Followup
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
	if len(f.identityErrs) > 0 {
		err := f.identityErrs[0]
		f.identityErrs = f.identityErrs[1:]
		return err
	}
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

// dispatchOK dispatches cmd and fails the test on any error. Nearly every
// test in this file wants exactly this, so hoisting it here keeps each
// test's own body to the branches that are actually its.
func dispatchOK(t *testing.T, h *Handlers, cmd ddiscord.Command) {
	t.Helper()
	if err := h.Dispatch(context.Background(), cmd); err != nil {
		t.Fatalf("dispatch %s: %v", cmd.Type, err)
	}
}

// requireOneCall fails the test unless calls has exactly one entry and it
// matches. The four moderation-type assertions in TestDispatchModerationTypes
// used to repeat "len(x) != 1 || x[0].Field != want" as their own branches;
// this makes each one a single call instead.
func requireOneCall[T any](t *testing.T, calls []T, match func(T) bool, label string) {
	t.Helper()
	if len(calls) != 1 || !match(calls[0]) {
		t.Fatalf("%s = %+v", label, calls)
	}
}

func TestDispatchPostEmbed(t *testing.T) {
	rest := &fakeRest{}
	h := &Handlers{Rest: rest}
	cmd := ddiscord.Command{
		Type: ddiscord.TypePostEmbed, GuildID: "g1", ChannelID: "c1",
		Payload: marshalPayload(t, ddiscord.EmbedPayload{Embed: ddiscord.Embed{Title: "hi"}}),
	}
	dispatchOK(t, h, cmd)
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
	dispatchOK(t, h, cmd)
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
	dispatchOK(t, h, cmd)
	if len(rest.edited) != 1 || rest.edited[0].ID != "m1" {
		t.Fatalf("edited = %+v", rest.edited)
	}
}

func TestDispatchModerationTypes(t *testing.T) {
	rest := &fakeRest{}
	h := &Handlers{Rest: rest}

	dispatchOK(t, h, ddiscord.Command{Type: ddiscord.TypeBanMember, GuildID: "g1", UserID: "u1"})
	dispatchOK(t, h, ddiscord.Command{Type: ddiscord.TypeKickMember, GuildID: "g1", UserID: "u2"})
	dispatchOK(t, h, ddiscord.Command{
		Type: ddiscord.TypeTimeoutMember, GuildID: "g1", UserID: "u3",
		Payload: marshalPayload(t, ddiscord.TimeoutPayload{UntilISO: "2026-01-01T00:00:00Z"}),
	})
	dispatchOK(t, h, ddiscord.Command{
		Type: ddiscord.TypeDeleteMessage, ChannelID: "c1",
		Payload: marshalPayload(t, ddiscord.DeletePayload{MessageID: "m9"}),
	})

	requireOneCall(t, rest.banned, func(m discapi.GuildMember) bool { return m.UserID == "u1" }, "banned")
	requireOneCall(t, rest.kicked, func(m discapi.GuildMember) bool { return m.UserID == "u2" }, "kicked")
	requireOneCall(t, rest.timeouts, func(m discapi.MemberTimeout) bool { return m.UntilISO != "" }, "timeouts")
	requireOneCall(t, rest.deleted, func(m discapi.Message) bool { return m.ID == "m9" }, "deleted")
}

func TestDispatchRoles(t *testing.T) {
	rest := &fakeRest{}
	h := &Handlers{Rest: rest}
	add := ddiscord.Command{Type: ddiscord.TypeAddRole, GuildID: "g1", UserID: "u1", Payload: marshalPayload(t, ddiscord.RolePayload{RoleID: "r1"})}
	rem := ddiscord.Command{Type: ddiscord.TypeRemoveRole, GuildID: "g1", UserID: "u1", Payload: marshalPayload(t, ddiscord.RolePayload{RoleID: "r1"})}
	dispatchOK(t, h, add)
	dispatchOK(t, h, rem)
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
	dispatchOK(t, h, ddiscord.Command{Type: ddiscord.TypeStripRoles})
	dispatchOK(t, h, ddiscord.Command{Type: ddiscord.TypeLockdown})
}

// A premium apply must set BOTH halves in one call: the nickname needs
// CHANGE_NICKNAME, the avatar needs no permission, and sending them
// separately would leave a guild half-renamed whenever one of the two fails.
func TestSetGuildIdentityPremiumSendsNickAndAvatar(t *testing.T) {
	rest := &fakeRest{}
	h := &Handlers{Rest: rest}
	dispatchOK(t, h, ddiscord.Command{
		Type: ddiscord.TypeSetGuildIdentity, GuildID: "g1",
		Payload: mustMarshal(t, ddiscord.IdentityPayload{Identity: ddiscord.GuildIdentity{Premium: true}}),
	})
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
	dispatchOK(t, h, ddiscord.Command{
		Type: ddiscord.TypeSetGuildIdentity, GuildID: "g1",
		Payload: mustMarshal(t, ddiscord.IdentityPayload{Identity: ddiscord.GuildIdentity{Premium: false}}),
	})
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

type fakeReauth struct {
	marked  []string
	cleared []string
}

func (f *fakeReauth) MarkNeedsReauth(_ context.Context, g string) error {
	f.marked = append(f.marked, g)
	return nil
}
func (f *fakeReauth) ClearNeedsReauth(_ context.Context, g string) error {
	f.cleared = append(f.cleared, g)
	return nil
}

func premiumIdentityCommand(t *testing.T) ddiscord.Command {
	t.Helper()
	return ddiscord.Command{
		Type: ddiscord.TypeSetGuildIdentity, GuildID: "g1",
		Payload: mustMarshal(t, ddiscord.IdentityPayload{Identity: ddiscord.GuildIdentity{Premium: true}}),
	}
}

// A guild installed before CHANGE_NICKNAME refuses the whole call, avatar
// included. Retrying without the nick still lands the premium avatar, and
// the refusal is recorded so the dashboard can ask for a re-authorization.
func TestSetGuildIdentityForbiddenFallsBackToAvatarOnly(t *testing.T) {
	rest := &fakeRest{identityErrs: []error{discapi.ErrForbidden}}
	reauth := &fakeReauth{}
	h := &Handlers{Rest: rest, Reauth: reauth}
	dispatchOK(t, h, premiumIdentityCommand(t))

	if len(rest.identities) != 2 {
		t.Fatalf("calls = %d, want 2 (nick+avatar, then avatar only)", len(rest.identities))
	}
	retry := rest.identities[1]
	if retry.Nick != nil {
		t.Fatal("retry still carried a nick")
	}
	if retry.AvatarDataURI == nil {
		t.Fatal("retry dropped the avatar too, so the guild gets nothing")
	}
	requireOneCall(t, reauth.marked, func(g string) bool { return g == "g1" }, "marked")
}

// A permission error must not nack: it will refuse identically forever, and
// redelivering it just burns the shared per-token budget.
func TestSetGuildIdentityForbiddenDoesNotRetryForever(t *testing.T) {
	rest := &fakeRest{identityErrs: []error{discapi.ErrForbidden}}
	h := &Handlers{Rest: rest, Reauth: &fakeReauth{}}
	if err := h.Dispatch(context.Background(), premiumIdentityCommand(t)); err != nil {
		t.Fatalf("a frozen permission was surfaced as a retryable error: %v", err)
	}
}

// A success is the only proof the permission arrived, so it clears the flag.
func TestSetGuildIdentitySuccessClearsReauth(t *testing.T) {
	reauth := &fakeReauth{}
	h := &Handlers{Rest: &fakeRest{}, Reauth: reauth}
	dispatchOK(t, h, premiumIdentityCommand(t))
	if len(reauth.cleared) != 1 {
		t.Fatalf("cleared = %v, want one entry", reauth.cleared)
	}
}

// A non-permission failure must still nack, or a transient Discord blip
// would silently drop the identity change.
func TestSetGuildIdentityOtherErrorStillFails(t *testing.T) {
	rest := &fakeRest{identityErrs: []error{errors.New("boom")}}
	h := &Handlers{Rest: rest, Reauth: &fakeReauth{}}
	if err := h.Dispatch(context.Background(), premiumIdentityCommand(t)); err == nil {
		t.Fatal("transient error was swallowed")
	}
}
