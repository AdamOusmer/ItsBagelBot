// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package commands_test

import (
	"context"
	"strconv"
	"testing"

	"ItsBagelBot/app/discord/outgress/internal/commands"
	"ItsBagelBot/app/discord/outgress/internal/kv"
	discapi "ItsBagelBot/internal/discordapi"
	ddiscord "ItsBagelBot/internal/domain/discord"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type undoStore int

const (
	noUndoStore undoStore = iota
	workingUndoStore
	failingUndoStore
)

type roleStrip struct {
	reason string
	roles  []string
}

func (s roleStrip) calls() []call {
	var out []call
	for _, id := range s.roles {
		role := discapi.MemberRole{GuildID: "g1", UserID: "u1", RoleID: id}
		out = append(out, call{"RemoveMemberRoleWithReason", auditedRole{Role: role, Reason: s.reason}})
	}
	return out
}

func raidRest() *fakeRest {
	return &fakeRest{
		members: map[string]discapi.GuildMemberInfo{
			"u1":  {Roles: []string{"g1", "r-mod", "r-boost", "r-vip", "r-admin"}},
			"bot": {Roles: []string{"r-bot"}},
		},
		guildRoles: []discapi.Snowflake{
			{ID: "r-mod", Position: 3},
			{ID: "r-boost", Position: 4, Managed: true},
			{ID: "r-vip", Position: 2},
			{ID: "r-bot", Position: 9, Managed: true},
			{ID: "r-admin", Position: 10},
		},
		guild: discapi.Snowflake{ID: "g1", VerificationLevel: 1},
		channels: []discapi.ChannelInfo{
			{ID: "c-text", Type: ddiscord.ChannelText, ParentID: "cat1", PermissionOverwrites: []discapi.PermissionOverwrite{
				{ID: "g1", Type: 0, Allow: "1024", Deny: "0"},
			}},
			{ID: "c-news", Type: ddiscord.ChannelNews, ParentID: "cat1"},
			{ID: "c-forum", Type: ddiscord.ChannelForum, ParentID: "cat1"},
			{ID: "c-voice", Type: ddiscord.ChannelVoice, ParentID: "cat1"},
			{ID: "c-other", Type: ddiscord.ChannelText, ParentID: "cat2"},
			{ID: "c-garbled", Type: ddiscord.ChannelText, ParentID: "cat3", PermissionOverwrites: []discapi.PermissionOverwrite{
				{ID: "g1", Type: 0, Allow: "not-a-bitfield", Deny: "0"},
			}},
		},
	}
}

func raidHandlers(rest *fakeRest, undo undoStore, seed *kv.LockdownState) (*commands.Handlers, *fakeLockdowns) {
	store := &fakeLockdowns{state: map[kv.GuildID]kv.LockdownState{}}
	if seed != nil {
		store.state["g1"] = *seed
	}
	h := &commands.Handlers{Rest: rest, ApplicationID: "bot", Log: zap.NewNop()}
	if undo == failingUndoStore {
		store.putErr = errBoom
	}
	if undo != noUndoStore {
		h.Lockdown = store
	}
	return h, store
}

func TestStripRoles(t *testing.T) {
	cases := []struct {
		name     string
		reason   string
		roleErrs map[string]error
		errs     map[op][]error
		want     roleStrip
		wantErr  error
	}{{
		name: "TestStripRolesSkipsManagedEveryoneAndHigherRoles",
		want: roleStrip{roles: []string{"r-mod", "r-vip"}},
	}, {
		name:   "TestStripRolesCarriesTheAuditReason",
		reason: "raid response",
		want:   roleStrip{reason: "raid response", roles: []string{"r-mod", "r-vip"}},
	}, {
		name:     "TestStripRolesSkipsForbiddenRoleAndContinues",
		roleErrs: map[string]error{"r-mod": discapi.ErrForbidden},
		want:     roleStrip{roles: []string{"r-vip"}},
	}, {
		name:     "TestStripRolesReturnsRetryableFailure",
		roleErrs: map[string]error{"r-mod": discapi.ErrRateLimited},
		want:     roleStrip{roles: []string{"r-vip"}},
		wantErr:  discapi.ErrRateLimited,
	}, {
		name:    "TestStripRolesFailsWhenLookupsFail/member fetch",
		errs:    map[op][]error{"GetGuildMember": {discapi.ErrChannelNotFound}},
		wantErr: discapi.ErrChannelNotFound,
	}, {
		name:    "TestStripRolesFailsWhenLookupsFail/role list",
		errs:    map[op][]error{"ListGuildRoles": {discapi.ErrForbidden}},
		wantErr: discapi.ErrForbidden,
	}, {
		name:    "fails when the bot's own member lookup fails",
		errs:    map[op][]error{"GetGuildMember": {nil, discapi.ErrRateLimited}},
		wantErr: discapi.ErrRateLimited,
	}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rest := raidRest()
			rest.roleErrs, rest.errs = tc.roleErrs, tc.errs
			h, _ := raidHandlers(rest, noUndoStore, nil)

			err := h.Dispatch(context.Background(), ddiscord.Command{
				Type: ddiscord.TypeStripRoles, GuildID: "g1", UserID: "u1", Reason: tc.reason,
			})

			require.ErrorIs(t, err, tc.wantErr)
			require.Equal(t, tc.want.calls(), rest.calls)
		})
	}
}

func lockdown(t *testing.T, categories ...string) ddiscord.Command {
	t.Helper()
	return ddiscord.Command{Type: ddiscord.TypeLockdown, GuildID: "g1",
		Payload: encode(t, ddiscord.LockdownPayload{CategoryIDs: categories})}
}

func verification(level int) call {
	return call{"ModifyGuild", discapi.GuildPatch{Guild: discapi.Guild{ID: "g1"}, VerificationLevel: &level}}
}

func overwrite(channel string, o discapi.PermissionOverwrite) call {
	o.ID = "g1"
	return call{"SetChannelOverwrite", discapi.ChannelOverwrite{ChannelID: channel, Overwrite: o}}
}

func muted(allow int) discapi.PermissionOverwrite {
	return discapi.PermissionOverwrite{Allow: strconv.Itoa(allow), Deny: "2048"}
}

func displaced() *kv.LockdownState {
	return &kv.LockdownState{VerificationLevel: 1, EveryoneRoleID: "g1", Channels: []kv.LockdownChannel{
		{ChannelID: "c-text", Allow: "1024", Deny: "0"},
		{ChannelID: "c-news", Allow: "0", Deny: "0"},
		{ChannelID: "c-forum", Allow: "0", Deny: "0"},
	}}
}

type raidCase struct {
	name    string
	cmd     ddiscord.Command
	undo    undoStore
	seed    *kv.LockdownState
	errs    map[op][]error
	want    []call
	wantErr error
	saved   *kv.LockdownState
}

func lockdownCases(t *testing.T) []raidCase {
	highest := verification(discapi.GuildVerificationHighest)
	mutedAll := []call{highest, overwrite("c-text", muted(1024)), overwrite("c-news", muted(0)), overwrite("c-forum", muted(0))}
	return []raidCase{{
		name: "raises verification without categories or an undo store",
		cmd:  ddiscord.Command{Type: ddiscord.TypeLockdown, GuildID: "g1"},
		want: []call{highest},
	}, {
		name:  "mutes every postable channel in the chosen categories and records what it displaced",
		cmd:   lockdown(t, "cat1"),
		undo:  workingUndoStore,
		want:  mutedAll,
		saved: displaced(),
	}, {
		name:    "records the undo state before a refused verification bump and mutes nothing",
		cmd:     lockdown(t, "cat1"),
		undo:    workingUndoStore,
		errs:    map[op][]error{"ModifyGuild": {discapi.ErrForbidden}},
		wantErr: discapi.ErrForbidden,
		saved:   displaced(),
	}, {
		name:    "keeps muting past a refused channel and reports it",
		cmd:     lockdown(t, "cat1"),
		undo:    workingUndoStore,
		errs:    map[op][]error{"SetChannelOverwrite": {discapi.ErrRateLimited}},
		want:    []call{highest, overwrite("c-news", muted(0)), overwrite("c-forum", muted(0))},
		wantErr: discapi.ErrRateLimited,
		saved:   displaced(),
	}, {
		name:    "skips a channel with unreadable bits and mutes the rest",
		cmd:     lockdown(t, "cat1", "cat3"),
		want:    mutedAll,
		wantErr: strconv.ErrSyntax,
	}, {
		name: "locks down even when the undo store is down",
		cmd:  lockdown(t, "cat1"),
		undo: failingUndoStore,
		want: mutedAll,
	}}
}

func unlockCases() []raidCase {
	unlock := ddiscord.Command{Type: ddiscord.TypeUnlock, GuildID: "g1"}
	restored := func(channel, allow string) call {
		return overwrite(channel, discapi.PermissionOverwrite{Allow: allow, Deny: "0"})
	}
	return []raidCase{{
		name: "unlock restores the displaced level and overwrites, then forgets them",
		cmd:  unlock,
		undo: workingUndoStore,
		seed: displaced(),
		want: []call{verification(1), restored("c-text", "1024"), restored("c-news", "0"), restored("c-forum", "0")},
	}, {
		name: "unlock without a recorded lockdown changes nothing",
		cmd:  unlock,
		undo: workingUndoStore,
	}, {
		name:    "unlock keeps the undo state when a restore fails",
		cmd:     unlock,
		undo:    workingUndoStore,
		seed:    displaced(),
		errs:    map[op][]error{"SetChannelOverwrite": {discapi.ErrRateLimited}},
		want:    []call{verification(1), restored("c-news", "0"), restored("c-forum", "0")},
		wantErr: discapi.ErrRateLimited,
		saved:   displaced(),
	}}
}

func TestLockdownAndUnlock(t *testing.T) {
	for _, tc := range append(lockdownCases(t), unlockCases()...) {
		t.Run(tc.name, func(t *testing.T) {
			rest := raidRest()
			rest.errs = tc.errs
			h, store := raidHandlers(rest, tc.undo, tc.seed)

			err := h.Dispatch(context.Background(), tc.cmd)

			require.ErrorIs(t, err, tc.wantErr)
			require.Equal(t, tc.want, rest.calls)
			require.Equal(t, tc.saved, store.saved())
		})
	}
}
