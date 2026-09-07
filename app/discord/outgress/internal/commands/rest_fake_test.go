// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package commands

import (
	"context"
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

	member        discapi.GuildMemberInfo
	members       map[string]discapi.GuildMemberInfo
	memberErr     error
	guildRoles    []discapi.Snowflake
	guildRolesErr error
	// roleRemReasons is the X-Audit-Log-Reason each removal carried, in the
	// same order as roleRems.
	roleRemReasons []string
	// removeErrs is role id -> the error that removal answers with.
	removeErrs     map[string]error
	fullChannels   []discapi.ChannelInfo
	guildPatches   []discapi.GuildPatch
	guildPatchErrs []error
	overwrites     []discapi.ChannelOverwrite
	overwriteErr   error
	guild          discapi.Snowflake
	guildErr       error
}

func (f *fakeRest) GetGuildMember(_ context.Context, m discapi.GuildMember) (discapi.GuildMemberInfo, error) {
	if f.memberErr != nil {
		return discapi.GuildMemberInfo{}, f.memberErr
	}
	if info, ok := f.members[m.UserID]; ok {
		return info, nil
	}
	return f.member, nil
}

func (f *fakeRest) ListGuildRoles(_ context.Context, _ discapi.Guild) ([]discapi.Snowflake, error) {
	return f.guildRoles, f.guildRolesErr
}

func (f *fakeRest) ListGuildChannelsFull(_ context.Context, _ discapi.Guild) ([]discapi.ChannelInfo, error) {
	return f.fullChannels, nil
}

func (f *fakeRest) ModifyGuild(_ context.Context, patch discapi.GuildPatch) error {
	if len(f.guildPatchErrs) > 0 {
		err := f.guildPatchErrs[0]
		f.guildPatchErrs = f.guildPatchErrs[1:]
		if err != nil {
			return err
		}
	}
	f.guildPatches = append(f.guildPatches, patch)
	return nil
}

func (f *fakeRest) GetGuild(_ context.Context, _ discapi.Guild) (discapi.Snowflake, error) {
	return f.guild, f.guildErr
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

func (f *fakeRest) RemoveMemberRoleWithReason(_ context.Context, r discapi.MemberRole, reason string) error {
	if err := f.removeErrs[r.RoleID]; err != nil {
		return err
	}
	f.roleRems = append(f.roleRems, r)
	f.roleRemReasons = append(f.roleRemReasons, reason)
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

// dispatchAll dispatches every command through one Handlers over a fresh fake
// and hands the fake back. The five dispatch cases below opened with the same
// three lines -- build a fake, wrap it, dispatch -- and only differ in the
// commands, so that is all they say now.
func dispatchAll(t *testing.T, cmds ...ddiscord.Command) *fakeRest {
	t.Helper()
	rest := &fakeRest{}
	h := &Handlers{Rest: rest}
	for _, cmd := range cmds {
		dispatchOK(t, h, cmd)
	}
	return rest
}
