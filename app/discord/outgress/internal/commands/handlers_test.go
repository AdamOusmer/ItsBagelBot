// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package commands

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"ItsBagelBot/app/discord/outgress/internal/kv"
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

	member       discapi.GuildMemberInfo
	memberErr    error
	guildRoles   []discapi.Snowflake
	fullChannels []discapi.ChannelInfo
	guildPatches []discapi.GuildPatch
	overwrites   []discapi.ChannelOverwrite
	overwriteErr error
}

func (f *fakeRest) GetGuildMember(_ context.Context, _ discapi.GuildMember) (discapi.GuildMemberInfo, error) {
	return f.member, f.memberErr
}
func (f *fakeRest) ListGuildRoles(_ context.Context, _ discapi.Guild) ([]discapi.Snowflake, error) {
	return f.guildRoles, nil
}
func (f *fakeRest) ListGuildChannelsFull(_ context.Context, _ discapi.Guild) ([]discapi.ChannelInfo, error) {
	return f.fullChannels, nil
}
func (f *fakeRest) ModifyGuild(_ context.Context, patch discapi.GuildPatch) error {
	f.guildPatches = append(f.guildPatches, patch)
	return nil
}
func (f *fakeRest) SetChannelOverwrite(_ context.Context, o discapi.ChannelOverwrite) error {
	f.overwrites = append(f.overwrites, o)
	return f.overwriteErr
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

// StripRoles must remove every role the bot CAN remove and skip the ones it
// cannot: a managed role (a bot's own, a booster role) is refused by Discord
// with a 403, and @everyone -- whose id equals the guild id -- is not a
// grantable role at all.
func TestStripRolesSkipsManagedAndEveryone(t *testing.T) {
	rest := &fakeRest{
		member: discapi.GuildMemberInfo{Roles: []string{"g1", "r-mod", "r-bot", "r-vip"}},
		guildRoles: []discapi.Snowflake{
			{ID: "r-mod"}, {ID: "r-bot", Managed: true}, {ID: "r-vip"},
		},
	}
	h := &Handlers{Rest: rest, Log: testLogger()}

	dispatchOK(t, h, ddiscord.Command{Type: ddiscord.TypeStripRoles, GuildID: "g1", UserID: "u1"})

	var got []string
	for _, r := range rest.roleRems {
		got = append(got, r.RoleID)
	}
	want := []string{"r-mod", "r-vip"}
	if !slices.Equal(got, want) {
		t.Fatalf("removed = %v, want %v", got, want)
	}
}

// A lockdown with no categories is still the useful half: the verification
// bump is what stops new accounts at the door.
func TestLockdownRaisesVerificationLevel(t *testing.T) {
	rest := &fakeRest{}
	h := &Handlers{Rest: rest, Log: testLogger()}

	dispatchOK(t, h, ddiscord.Command{Type: ddiscord.TypeLockdown, GuildID: "g1"})

	if len(rest.guildPatches) != 1 {
		t.Fatalf("guild patches = %d, want 1", len(rest.guildPatches))
	}
	got := rest.guildPatches[0]
	if got.VerificationLevel == nil || *got.VerificationLevel != discapi.GuildVerificationHighest {
		t.Fatalf("verification level = %v, want %d", got.VerificationLevel, discapi.GuildVerificationHighest)
	}
	if len(rest.overwrites) != 0 {
		t.Fatalf("overwrites = %d, want 0 with no categories", len(rest.overwrites))
	}
}

// Muting must ADD the SEND deny to whatever the channel already denies and
// clear it from the allow, because Discord's overwrite write replaces the
// whole overwrite -- a bare deny would drop the VIEW allow that makes a
// gated channel visible to its tier.
func TestLockdownMutesTextChannelsInCategoriesOnly(t *testing.T) {
	rest := &fakeRest{fullChannels: []discapi.ChannelInfo{
		{ID: "c-text", Type: ddiscord.ChannelText, ParentID: "cat1", PermissionOverwrites: []discapi.PermissionOverwrite{
			{ID: "g1", Type: 0, Allow: "1024", Deny: "0"},
		}},
		{ID: "c-voice", Type: ddiscord.ChannelVoice, ParentID: "cat1"},
		{ID: "c-other", Type: ddiscord.ChannelText, ParentID: "cat2"},
	}}
	h := &Handlers{Rest: rest, Log: testLogger()}

	dispatchOK(t, h, ddiscord.Command{
		Type: ddiscord.TypeLockdown, GuildID: "g1",
		Payload: mustMarshal(t, ddiscord.LockdownPayload{CategoryIDs: []string{"cat1"}}),
	})

	if len(rest.overwrites) != 1 {
		t.Fatalf("overwrites = %d, want 1", len(rest.overwrites))
	}
	got := rest.overwrites[0]
	if got.ChannelID != "c-text" {
		t.Fatalf("channel = %q, want c-text", got.ChannelID)
	}
	if got.Overwrite.ID != "g1" {
		t.Fatalf("overwrite target = %q, want the guild id (@everyone)", got.Overwrite.ID)
	}
	if got.Overwrite.Allow != "1024" || got.Overwrite.Deny != "2048" {
		t.Fatalf("allow/deny = %q/%q, want 1024/2048", got.Overwrite.Allow, got.Overwrite.Deny)
	}
}

// A failed lockdown must NACK. The old no-op ACKed the message, so a
// lockdown the bot lacked MANAGE_GUILD for looked delivered.
func TestLockdownSurfacesOverwriteFailure(t *testing.T) {
	rest := &fakeRest{
		overwriteErr: errors.New("403 missing permissions"),
		fullChannels: []discapi.ChannelInfo{{ID: "c1", Type: ddiscord.ChannelText, ParentID: "cat1"}},
	}
	h := &Handlers{Rest: rest, Log: testLogger()}

	err := h.Dispatch(context.Background(), ddiscord.Command{
		Type: ddiscord.TypeLockdown, GuildID: "g1",
		Payload: mustMarshal(t, ddiscord.LockdownPayload{CategoryIDs: []string{"cat1"}}),
	})
	if err == nil {
		t.Fatal("a refused overwrite must surface so the lane redelivers")
	}
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

func (f *fakeReauth) MarkNeedsReauth(_ context.Context, g kv.GuildID) error {
	f.marked = append(f.marked, string(g))
	return nil
}
func (f *fakeReauth) ClearNeedsReauth(_ context.Context, g kv.GuildID) error {
	f.cleared = append(f.cleared, string(g))
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
