// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules_test

import (
	"context"
	"errors"
	"testing"

	"ItsBagelBot/app/discord/engine/internal/decode"
	"ItsBagelBot/app/discord/engine/module"
	"ItsBagelBot/app/discord/engine/modules"
	"ItsBagelBot/internal/discordstore"
	ddiscord "ItsBagelBot/internal/domain/discord"
	discordoutgress "ItsBagelBot/internal/domain/rpc/discordoutgress"
	"ItsBagelBot/pkg/codec"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type voiceRPC struct {
	calls     []string
	created   []discordoutgress.ChannelCreateRequest
	modified  []discordoutgress.ChannelModifyRequest
	modifyErr string
	moveErr   string
	deleteErr string
}

func (v *voiceRPC) CreateChannel(_ context.Context, req discordoutgress.ChannelCreateRequest) (discordoutgress.ChannelCreateReply, error) {
	v.calls = append(v.calls, "create")
	v.created = append(v.created, req)
	return discordoutgress.ChannelCreateReply{ChannelID: "room1"}, nil
}

func (v *voiceRPC) DeleteChannel(_ context.Context, req discordoutgress.ChannelDeleteRequest) (discordoutgress.ChannelDeleteReply, error) {
	v.calls = append(v.calls, "delete:"+req.ChannelID)
	return discordoutgress.ChannelDeleteReply{Error: v.deleteErr}, nil
}

func (v *voiceRPC) ModifyChannel(_ context.Context, req discordoutgress.ChannelModifyRequest) (discordoutgress.ChannelModifyReply, error) {
	v.calls = append(v.calls, "modify")
	v.modified = append(v.modified, req)
	return discordoutgress.ChannelModifyReply{Error: v.modifyErr}, nil
}

func (v *voiceRPC) MoveMember(context.Context, discordoutgress.MemberMoveRequest) (discordoutgress.MemberMoveReply, error) {
	v.calls = append(v.calls, "move")
	return discordoutgress.MemberMoveReply{Error: v.moveErr}, nil
}

type trackFails struct{ *discordstore.Mem }

func (trackFails) TrackClone(context.Context, discordstore.Clone) error {
	return errors.New("valkey down")
}

type storeDown struct{ *discordstore.Mem }

func (storeDown) UpdateVoiceOccupancy(context.Context, discordstore.VoiceSeat) discordstore.VoiceMove {
	return discordstore.VoiceMove{}
}

type voiceRig struct {
	t    *testing.T
	mem  *discordstore.Mem
	rpc  *voiceRPC
	mod  module.Module
	cfg  ddiscord.Config
	sent []ddiscord.Command
}

func newVoiceRig(t *testing.T, store func(*discordstore.Mem) discordstore.Store) *voiceRig {
	t.Helper()
	r := &voiceRig{t: t, mem: discordstore.NewMem(), rpc: &voiceRPC{}}
	r.cfg = ddiscord.Config{GuildID: "g1", VoiceEnabled: "on", VoiceHubID: "hub", ModsRoleID: "m"}
	var s discordstore.Store = r.mem
	if store != nil {
		s = store(r.mem)
	}
	r.mod = modules.Voice(s, r.rpc, zap.NewNop())
	return r
}

func (r *voiceRig) state(user, channel string) {
	r.t.Helper()
	var ev decode.VoiceEvent
	ev.GuildID, ev.UserID, ev.ChannelID = "g1", user, channel
	ev.Member.User = decode.UserRef{ID: user, Username: "Ada"}
	raw, err := codec.Marshal(ev)
	require.NoError(r.t, err)
	c := &module.Context{Event: ddiscord.Event{Raw: raw}, Config: r.cfg, Log: zap.NewNop()}
	require.NoError(r.t, r.mod.Events["VOICE_STATE_UPDATE"](context.Background(), c, func(cmd ddiscord.Command) { r.sent = append(r.sent, cmd) }))
}

func (r *voiceRig) slash(sub decode.InteractionOption, a actor) string {
	r.t.Helper()
	in := a.in("room1")
	in.Data.Options = []decode.InteractionOption{sub}
	return ephemeralText(r.t, runHandler(r.t, r.mod.Slash["voice"], r.cfg, in))
}

func (r *voiceRig) trackRoom() {
	r.t.Helper()
	require.NoError(r.t, r.mem.TrackClone(context.Background(), discordstore.Clone{ChannelID: "room1", GuildID: "g1", OwnerID: "u1"}))
}

func (r *voiceRig) cloneTracked() bool {
	_, ok := r.mem.Clone(context.Background(), discordstore.Channel{ID: "room1"})
	return ok
}

func intOption(name string, n string) decode.InteractionOption {
	return decode.InteractionOption{Name: name, Type: 4, Value: codec.RawMessage(n)}
}

func TestVoiceHubClonesOnlyWhenEntered(t *testing.T) {
	r := newVoiceRig(t, nil)

	r.state("u1", "hub")
	r.state("u1", "hub")
	r.state("u1", "hub")

	require.Equal(t, []string{"create", "move"}, r.rpc.calls, "mute or stream updates while in the hub must not clone again")
	require.Len(t, r.sent, 1, "one room panel")
}

func TestVoiceCloneUsesConfiguredRoom(t *testing.T) {
	cases := []struct {
		name    string
		privacy string
		want    []string
	}{
		{"open", "", []string{"u1"}},
		{"locked", ddiscord.VoicePrivacyLocked, []string{"u1", "g1:1048576"}},
		{"hidden", ddiscord.VoicePrivacyHidden, []string{"u1", "g1:1024"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := newVoiceRig(t, nil)
			r.cfg.VoiceCategoryID, r.cfg.VoiceNameTemplate, r.cfg.VoiceUserLimit, r.cfg.VoicePrivacyMode = "cat1", "{owner}'s room", "4", tc.privacy

			r.state("u1", "hub")

			req := r.rpc.created[0]
			var got []string
			for _, o := range req.Overwrites {
				if o.Deny != "0" {
					got = append(got, o.ID+":"+o.Deny)
					continue
				}
				got = append(got, o.ID)
			}
			require.Equal(t, []any{"cat1", "Ada's room", 4, tc.want}, []any{req.ParentID, req.Name, req.UserLimit, got})
		})
	}
}

func TestVoiceCloneIsDeletedWhenTrackingOrMoveFails(t *testing.T) {
	cases := []struct {
		name  string
		store func(*discordstore.Mem) discordstore.Store
		move  string
		calls []string
	}{
		{"tracking fails", func(m *discordstore.Mem) discordstore.Store { return trackFails{m} }, "", []string{"create", "delete:room1"}},
		{"move fails", nil, "user left voice", []string{"create", "move", "delete:room1"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := newVoiceRig(t, tc.store)
			r.rpc.moveErr = tc.move

			r.state("u1", "hub")

			require.Equal(t, tc.calls, r.rpc.calls)
			require.False(t, r.cloneTracked())
			require.Empty(t, r.sent, "no panel for a room that was rolled back")
		})
	}
}

func TestVoiceEmptyCloneIsDeletedBeforeItIsForgotten(t *testing.T) {
	r := newVoiceRig(t, nil)
	r.trackRoom()
	r.state("u1", "room1")
	r.rpc.deleteErr = "missing access"

	r.state("u1", "")
	require.True(t, r.cloneTracked(), "a failed delete keeps the clone tracked")

	r.rpc.deleteErr = ""
	r.state("u1", "room1")
	r.state("u1", "")
	require.False(t, r.cloneTracked())
	require.Equal(t, []string{"delete:room1", "delete:room1"}, r.rpc.calls)
}

func TestVoiceCommandsAnswerWithTheRealOutcome(t *testing.T) {
	owner := actor{id: "u1", name: "Ada", perms: "0"}
	cases := []struct {
		name      string
		sub       decode.InteractionOption
		modifyErr string
		want      string
	}{
		{"rename ok", subcommand("name", decode.InteractionOption{Name: "name", Value: codec.RawMessage(`"new"`)}), "", "Renamed."},
		{"rename fails", subcommand("name", decode.InteractionOption{Name: "name", Value: codec.RawMessage(`"new"`)}), "discord: forbidden: nope", "Could not change the name right now."},
		{"rename rate limited", subcommand("name", decode.InteractionOption{Name: "name", Value: codec.RawMessage(`"new"`)}), "discord: rate limited: slow", "Discord only allows 2 renames per 10 minutes. Try again later."},
		{"limit fails", subcommand("limit", intOption("count", "5")), "boom", "Could not change the user limit right now."},
		{"limit set", subcommand("limit", intOption("count", "5")), "", "User limit set to 5."},
		{"lock fails", subcommand("lock"), "boom", "Could not change the lock right now."},
		{"lock ok", subcommand("lock"), "", "Locked."},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := newVoiceRig(t, nil)
			r.trackRoom()
			r.rpc.modifyErr = tc.modifyErr

			require.Equal(t, tc.want, r.slash(tc.sub, owner))
		})
	}
}

func TestVoiceLimitZeroClearsTheLimit(t *testing.T) {
	r := newVoiceRig(t, nil)
	r.trackRoom()

	reply := r.slash(subcommand("limit", intOption("count", "0")), actor{id: "u1", name: "Ada", perms: "0"})

	require.Equal(t, "User limit cleared.", reply)
	require.NotNil(t, r.rpc.modified[0].UserLimit)
	require.Equal(t, 0, *r.rpc.modified[0].UserLimit)
}

func TestVoiceHubEntryDuringStoreFailureCreatesNothing(t *testing.T) {
	r := newVoiceRig(t, func(m *discordstore.Mem) discordstore.Store { return storeDown{m} })

	r.state("u1", "hub")

	require.Empty(t, r.rpc.calls)
}

func TestVoiceLockFollowsConfiguredPrivacy(t *testing.T) {
	owner := actor{id: "u1", name: "Ada", perms: "0"}
	cases := []struct {
		name    string
		privacy string
		sub     string
		deny    string
	}{
		{"hidden unlock stays hidden", ddiscord.VoicePrivacyHidden, "unlock", "1024"},
		{"hidden lock hides and blocks", ddiscord.VoicePrivacyHidden, "lock", "1049600"},
		{"open unlock has no deny", "", "unlock", ""},
		{"open lock denies connect", "", "lock", "1048576"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := newVoiceRig(t, nil)
			r.cfg.VoicePrivacyMode = tc.privacy
			r.trackRoom()

			r.slash(subcommand(tc.sub), owner)

			var denies []string
			for _, o := range r.rpc.modified[0].Overwrites[1:] {
				require.Equal(t, "g1", o.ID)
				denies = append(denies, o.Deny)
			}
			if tc.deny == "" {
				require.Empty(t, denies)
				return
			}
			require.Equal(t, []string{tc.deny}, denies)
		})
	}
}
