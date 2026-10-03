// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package commands_test

import (
	"context"
	"errors"
	"testing"

	"ItsBagelBot/app/discord/outgress/internal/kv"
	discapi "ItsBagelBot/internal/discordapi"
	"ItsBagelBot/pkg/codec"

	"github.com/stretchr/testify/require"
)

var errBoom = errors.New("boom")

type op string

type call struct {
	Op  op
	Arg any
}

type panelPost struct {
	Post    discapi.EmbedPost
	Buttons []discapi.Button
}

type messageEdit struct {
	Message discapi.Message
	Patch   discapi.MessagePatch
}

type auditedRole struct {
	Role   discapi.MemberRole
	Reason string
}

type fakeRest struct {
	channelGuilds map[string]string
	members       map[string]discapi.GuildMemberInfo
	guildRoles    []discapi.Snowflake
	channels      []discapi.ChannelInfo
	guild         discapi.Snowflake
	roleErrs      map[string]error
	errs          map[op][]error
	calls         []call
}

func (f *fakeRest) fail(o op) error {
	queue := f.errs[o]
	if len(queue) == 0 {
		return nil
	}
	f.errs[o] = queue[1:]
	return queue[0]
}

func (f *fakeRest) write(o op, arg any) error {
	if err := f.fail(o); err != nil {
		return err
	}
	f.calls = append(f.calls, call{Op: o, Arg: arg})
	return nil
}

func (f *fakeRest) GetChannel(_ context.Context, id string) (discapi.ChannelInfo, error) {
	guild, ok := f.channelGuilds[id]
	if !ok {
		guild = "g1"
	}
	return discapi.ChannelInfo{ID: id, GuildID: guild}, nil
}

func (f *fakeRest) GetGuildMember(_ context.Context, m discapi.GuildMember) (discapi.GuildMemberInfo, error) {
	return f.members[m.UserID], f.fail("GetGuildMember")
}

func (f *fakeRest) ListGuildRoles(context.Context, discapi.Guild) ([]discapi.Snowflake, error) {
	return f.guildRoles, f.fail("ListGuildRoles")
}

func (f *fakeRest) ListGuildChannelsFull(context.Context, discapi.Guild) ([]discapi.ChannelInfo, error) {
	return f.channels, f.fail("ListGuildChannelsFull")
}

func (f *fakeRest) GetGuild(context.Context, discapi.Guild) (discapi.Snowflake, error) {
	return f.guild, f.fail("GetGuild")
}

func (f *fakeRest) ModifyGuild(_ context.Context, patch discapi.GuildPatch) error {
	return f.write("ModifyGuild", patch)
}

func (f *fakeRest) SetChannelOverwrite(_ context.Context, o discapi.ChannelOverwrite) error {
	return f.write("SetChannelOverwrite", o)
}

func (f *fakeRest) SendChat(_ context.Context, post discapi.ChatPost) error {
	return f.write("SendChat", post)
}

func (f *fakeRest) SendEmbed(_ context.Context, post discapi.EmbedPost) (discapi.Message, error) {
	return discapi.Message{ChannelID: post.ChannelID, ID: "m1"}, f.write("SendEmbed", post)
}

func (f *fakeRest) SendPanel(_ context.Context, post discapi.EmbedPost, buttons []discapi.Button) (discapi.Message, error) {
	return discapi.Message{ChannelID: post.ChannelID, ID: "p1"}, f.write("SendPanel", panelPost{Post: post, Buttons: buttons})
}

func (f *fakeRest) EditMessage(_ context.Context, m discapi.Message, patch discapi.MessagePatch) error {
	return f.write("EditMessage", messageEdit{Message: m, Patch: patch})
}

func (f *fakeRest) DeleteMessage(_ context.Context, m discapi.Message) error {
	return f.write("DeleteMessage", m)
}

func (f *fakeRest) TimeoutMember(_ context.Context, t discapi.MemberTimeout) error {
	return f.write("TimeoutMember", t)
}

func (f *fakeRest) KickMember(_ context.Context, m discapi.GuildMember) error {
	return f.write("KickMember", m)
}

func (f *fakeRest) BanMember(_ context.Context, m discapi.GuildMember) error {
	return f.write("BanMember", m)
}

func (f *fakeRest) AddMemberRole(_ context.Context, r discapi.MemberRole) error {
	return f.write("AddMemberRole", r)
}

func (f *fakeRest) RemoveMemberRole(_ context.Context, r discapi.MemberRole) error {
	return f.write("RemoveMemberRole", r)
}

func (f *fakeRest) RemoveMemberRoleWithReason(_ context.Context, r discapi.MemberRole, reason string) error {
	if err := f.roleErrs[r.RoleID]; err != nil {
		return err
	}
	return f.write("RemoveMemberRoleWithReason", auditedRole{Role: r, Reason: reason})
}

func (f *fakeRest) ModifyCurrentMember(_ context.Context, m discapi.CurrentMember) error {
	return f.write("ModifyCurrentMember", m)
}

func (f *fakeRest) InteractionFollowup(_ context.Context, followup discapi.Followup) error {
	return f.write("InteractionFollowup", followup)
}

type fakeReauth struct {
	Marked  []kv.GuildID
	Cleared []kv.GuildID
}

func (f *fakeReauth) MarkNeedsReauth(_ context.Context, g kv.GuildID) error {
	f.Marked = append(f.Marked, g)
	return nil
}

func (f *fakeReauth) ClearNeedsReauth(_ context.Context, g kv.GuildID) error {
	f.Cleared = append(f.Cleared, g)
	return nil
}

type fakeLockdowns struct {
	state  map[kv.GuildID]kv.LockdownState
	putErr error
}

func (f *fakeLockdowns) PutLockdown(_ context.Context, g kv.GuildID, s kv.LockdownState) error {
	if f.putErr != nil {
		return f.putErr
	}
	f.state[g] = s
	return nil
}

func (f *fakeLockdowns) GetLockdown(_ context.Context, g kv.GuildID) (kv.LockdownState, bool) {
	s, ok := f.state[g]
	return s, ok
}

func (f *fakeLockdowns) DeleteLockdown(_ context.Context, g kv.GuildID) error {
	delete(f.state, g)
	return nil
}

func (f *fakeLockdowns) saved() *kv.LockdownState {
	s, ok := f.state["g1"]
	if !ok {
		return nil
	}
	return &s
}

func encode(t *testing.T, v any) []byte {
	t.Helper()
	raw, err := codec.Marshal(v)
	require.NoError(t, err)
	return raw
}
