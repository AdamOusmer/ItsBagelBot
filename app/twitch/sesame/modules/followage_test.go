// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"context"
	"errors"
	"testing"
	"time"

	"ItsBagelBot/app/twitch/sesame/engine"
	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/internal/domain/event/lane"
	"ItsBagelBot/internal/domain/outgress"
	"ItsBagelBot/internal/projection"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// builtinName is one of the two built-ins this file exercises.
//
// It is a defined type, and the two spellings are constants, because the same
// name has to key three different things — the command in the module, the
// projection row the toggle test writes, and the case in each table — and a
// typo in any one of them fails as "command not found" rather than as the
// mismatch it is.
type builtinName string

const (
	followageBuiltin  builtinName = "followage"
	accountAgeBuiltin builtinName = "accountage"
)

// builtinCommand pulls one command by name out of the followage built-in
// module, shared by the !followage and !accountage tests.
func builtinCommand(t *testing.T, d engine.Deps, name builtinName) module.Command {
	t.Helper()
	for _, cmd := range Followage(d).Commands {
		if cmd.Name == string(name) {
			return cmd
		}
	}
	t.Fatalf("%s command not found", name)
	return module.Command{}
}

func lookupContext() *module.Context {
	return &module.Context{Env: lane.Envelope{
		BroadcasterUserID: "5", ChatterUserID: "9", ChatterUserLogin: "viewer", ChatterUserName: "Viewer",
	}, BroadcasterID: 5}
}

type fakeFollowage struct {
	result engine.FollowageResult
	err    error
	got    struct{ broadcasterID, targetID, targetLogin string }
}

func (f *fakeFollowage) Lookup(_ context.Context, broadcasterID, targetID, targetLogin string) (engine.FollowageResult, error) {
	f.got = struct{ broadcasterID, targetID, targetLogin string }{broadcasterID, targetID, targetLogin}
	return f.result, f.err
}

// TestBuiltinDefaultsToChatter runs both commands with no argument and asserts
// each looks up the chatter (id "9"), replies with its formatted line, and
// carries the 15s cooldown.
func TestBuiltinDefaultsToChatter(t *testing.T) {
	cases := []struct {
		name     builtinName
		deps     engine.Deps
		want     string
		cooldown time.Duration
	}{
		{
			name: followageBuiltin,
			deps: func() engine.Deps {
				l := &fakeFollowage{result: engine.FollowageResult{TargetID: "9", UserFound: true, Following: true, FollowedAt: time.Now().Add(-40 * 24 * time.Hour)}}
				return engine.Deps{Followage: l, Log: zap.NewNop()}
			}(),
			want:     "@Viewer has followed for 1 month, 10 days.",
			cooldown: followageCooldown,
		},
		{
			name: accountAgeBuiltin,
			deps: func() engine.Deps {
				l := &fakeAccountAge{result: engine.AccountAgeResult{TargetID: "9", UserFound: true, CreatedAt: time.Now().Add(-400 * 24 * time.Hour)}}
				return engine.Deps{AccountAge: l, Log: zap.NewNop()}
			}(),
			want:     "@Viewer's account is 1 year, 1 month old.",
			cooldown: accountAgeCooldown,
		},
	}
	for _, tc := range cases {
		cmd := builtinCommand(t, tc.deps, tc.name)
		var col collector
		require.NoError(t, cmd.Run(context.Background(), lookupContext(), "", col.emit))
		require.Len(t, col.out, 1)
		assert.Equal(t, outgress.TypeChat, col.out[0].Type)
		assert.Equal(t, tc.want, col.out[0].Text)
		assert.Equal(t, tc.cooldown, cmd.Cooldown)
	}
}

func TestFollowageAcceptsTargetLogin(t *testing.T) {
	lookup := &fakeFollowage{result: engine.FollowageResult{TargetID: "10", UserFound: true}}
	cmd := builtinCommand(t, engine.Deps{Followage: lookup, Log: zap.NewNop()}, followageBuiltin)
	var col collector
	require.NoError(t, cmd.Run(context.Background(), lookupContext(), "@Other ignored", col.emit))
	require.Len(t, col.out, 1)
	assert.Equal(t, "Other", lookup.got.targetLogin)
	assert.Equal(t, "@Other is not following this channel.", col.out[0].Text)
}

// TestBuiltinLookupFailureRepliesUnavailable and TestBuiltinToggleSuppresses
// cover both !followage and !accountage in one table each: the two commands
// share the same failure and toggle behavior, only the wiring differs.
func TestBuiltinLookupFailureRepliesUnavailable(t *testing.T) {
	cases := []struct {
		name builtinName
		deps engine.Deps
		want string
	}{
		{followageBuiltin, engine.Deps{Followage: &fakeFollowage{err: errors.New("boom")}, Log: zap.NewNop()}, "Followage is unavailable right now."},
		{accountAgeBuiltin, engine.Deps{AccountAge: &fakeAccountAge{err: errors.New("boom")}, Log: zap.NewNop()}, "Account age is unavailable right now."},
	}
	for _, tc := range cases {
		cmd := builtinCommand(t, tc.deps, tc.name)
		var col collector
		require.NoError(t, cmd.Run(context.Background(), lookupContext(), "", col.emit))
		require.Len(t, col.out, 1)
		assert.Equal(t, tc.want, col.out[0].Text)
	}
}

func TestBuiltinToggleSuppressesCommand(t *testing.T) {
	for _, name := range []builtinName{followageBuiltin, accountAgeBuiltin} {
		reader := clipReader{modules: []projection.ModuleView{{Name: string(name), IsEnabled: false}}}
		cmd := builtinCommand(t, engine.Deps{Proj: reader, Log: zap.NewNop()}, name)
		var col collector
		require.NoError(t, cmd.Run(context.Background(), lookupContext(), "", col.emit))
		assert.Empty(t, col.out)
	}
}

type fakeAccountAge struct {
	result engine.AccountAgeResult
	err    error
	got    struct{ targetID, targetLogin string }
}

func (f *fakeAccountAge) Lookup(_ context.Context, targetID, targetLogin string) (engine.AccountAgeResult, error) {
	f.got = struct{ targetID, targetLogin string }{targetID, targetLogin}
	return f.result, f.err
}

func TestAccountAgeAcceptsTargetLogin(t *testing.T) {
	lookup := &fakeAccountAge{result: engine.AccountAgeResult{UserFound: false}}
	cmd := builtinCommand(t, engine.Deps{AccountAge: lookup, Log: zap.NewNop()}, accountAgeBuiltin)
	var col collector
	require.NoError(t, cmd.Run(context.Background(), lookupContext(), "@Ghost ignored", col.emit))
	require.Len(t, col.out, 1)
	assert.Equal(t, "Ghost", lookup.got.targetLogin)
	assert.Equal(t, "@Ghost is not a Twitch user.", col.out[0].Text)
}
