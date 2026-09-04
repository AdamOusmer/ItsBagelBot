// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"context"
	"errors"
	"strings"
	"testing"

	"ItsBagelBot/app/twitch/sesame/engine"
	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/internal/domain/event/lane"
	"ItsBagelBot/internal/domain/outgress"
	"ItsBagelBot/internal/projection"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// --- test doubles ---

type fakeCommandManager struct {
	upsertCalls []upsertCall
	deleteCalls []deleteCall
	upsertErr   error
	deleteErr   error
}

type upsertCall struct{ UserID, Name, Response string }
type deleteCall struct{ UserID, Name string }

func (f *fakeCommandManager) Upsert(_ context.Context, userID, name, response string) error {
	f.upsertCalls = append(f.upsertCalls, upsertCall{userID, name, response})
	return f.upsertErr
}

func (f *fakeCommandManager) Delete(_ context.Context, userID, name string) error {
	f.deleteCalls = append(f.deleteCalls, deleteCall{userID, name})
	return f.deleteErr
}

type fakeProj struct {
	commands map[string]projection.Command
	modules  []projection.ModuleView
}

func (f *fakeProj) User(context.Context, uint64) (projection.User, error) {
	return projection.User{}, nil
}

func (f *fakeProj) Modules(context.Context, uint64) (map[string]projection.ModuleView, error) {
	return projection.ModuleMap(f.modules), nil
}

func (f *fakeProj) Module(ctx context.Context, id uint64, name string) (projection.ModuleView, bool, error) {
	views, err := f.Modules(ctx, id)
	if err != nil {
		return projection.ModuleView{}, false, err
	}
	view, ok := views[name]
	return view, ok, nil
}

func (f *fakeProj) Command(_ context.Context, _ uint64, name string) (projection.Command, bool, error) {
	cmd, ok := f.commands[name]
	return cmd, ok, nil
}

func cmdDeps(proj projection.Reader, cmds engine.CommandManager) engine.Deps {
	return engine.Deps{
		Proj:     proj,
		Commands: cmds,
		Log:      zap.NewNop(),
	}
}

// cmdCtx builds a moderator chatter context: command management is mod-gated, so
// the add/edit/remove tests run as a mod.
func cmdCtx(chatterLogin, text string) *module.Context {
	return &module.Context{
		Env: lane.Envelope{
			Type:                 "channel.chat.message",
			BroadcasterUserID:    "100",
			BroadcasterUserLogin: "streamer",
			ChatterUserID:        "42",
			ChatterUserLogin:     chatterLogin,
			Text:                 text,
			Badges:               []lane.Badge{{SetID: "moderator"}},
		},
		Regress:       module.RegressPremium,
		BroadcasterID: 100,
		Log:           zap.NewNop(),
	}
}

// viewerCtx is a plain (non-mod) chatter: it may fetch the public link but must
// not manage commands.
func viewerCtx(chatterLogin, text string) *module.Context {
	c := cmdCtx(chatterLogin, text)
	c.Env.Badges = nil
	return c
}

// --- !cmd add ---

func TestCmdAddSuccess(t *testing.T) {
	cmds := &fakeCommandManager{}
	proj := &fakeProj{commands: map[string]projection.Command{}}
	m := Cmd(cmdDeps(proj, cmds))
	cmd := findCmd(t, m, "cmd")

	var col collector
	require.NoError(t, cmd.Run(context.Background(), cmdCtx("alice", "!cmd add hello Hello world!"), "add hello Hello world!", col.emit))

	require.Len(t, cmds.upsertCalls, 1)
	assert.Equal(t, "100", cmds.upsertCalls[0].UserID)
	assert.Equal(t, "hello", cmds.upsertCalls[0].Name)
	assert.Equal(t, "Hello world!", cmds.upsertCalls[0].Response)

	require.Len(t, col.out, 1)
	assert.Equal(t, outgress.TypeChat, col.out[0].Type)
	assert.Equal(t, "100", col.out[0].BroadcasterID)
	assert.Contains(t, col.out[0].Text, "@alice")
	assert.Contains(t, col.out[0].Text, "hello")
	assert.Contains(t, col.out[0].Text, "added")
}

func TestCmdAddAlreadyExists(t *testing.T) {
	cmds := &fakeCommandManager{}
	proj := &fakeProj{commands: map[string]projection.Command{
		"hello": {Name: "hello", Response: "Hi!"},
	}}
	m := Cmd(cmdDeps(proj, cmds))
	cmd := findCmd(t, m, "cmd")

	var col collector
	require.NoError(t, cmd.Run(context.Background(), cmdCtx("bob", "!cmd add hello New"), "add hello New", col.emit))

	assert.Empty(t, cmds.upsertCalls, "should not upsert when command exists")
	require.Len(t, col.out, 1)
	assert.Contains(t, col.out[0].Text, "already exists")
	assert.Contains(t, col.out[0].Text, "!cmd edit")
}

func TestCmdAddMissingResponse(t *testing.T) {
	cmds := &fakeCommandManager{}
	proj := &fakeProj{commands: map[string]projection.Command{}}
	m := Cmd(cmdDeps(proj, cmds))
	cmd := findCmd(t, m, "cmd")

	var col collector
	require.NoError(t, cmd.Run(context.Background(), cmdCtx("alice", "!cmd add hello"), "add hello", col.emit))

	assert.Empty(t, cmds.upsertCalls)
	require.Len(t, col.out, 1)
	assert.Contains(t, col.out[0].Text, "response")
}

func TestCmdAddMissingName(t *testing.T) {
	cmds := &fakeCommandManager{}
	proj := &fakeProj{commands: map[string]projection.Command{}}
	m := Cmd(cmdDeps(proj, cmds))
	cmd := findCmd(t, m, "cmd")

	var col collector
	require.NoError(t, cmd.Run(context.Background(), cmdCtx("alice", "!cmd add"), "add", col.emit))

	assert.Empty(t, cmds.upsertCalls)
	require.Len(t, col.out, 1)
	assert.Contains(t, col.out[0].Text, "Usage")
}

// --- !cmd edit ---

func TestCmdEditSuccess(t *testing.T) {
	cmds := &fakeCommandManager{}
	proj := &fakeProj{commands: map[string]projection.Command{
		"hello": {Name: "hello", Response: "Hi!"},
	}}
	m := Cmd(cmdDeps(proj, cmds))
	cmd := findCmd(t, m, "cmd")

	var col collector
	require.NoError(t, cmd.Run(context.Background(), cmdCtx("alice", "!cmd edit hello Updated!"), "edit hello Updated!", col.emit))

	require.Len(t, cmds.upsertCalls, 1)
	assert.Equal(t, "hello", cmds.upsertCalls[0].Name)
	assert.Equal(t, "Updated!", cmds.upsertCalls[0].Response)

	require.Len(t, col.out, 1)
	assert.Contains(t, col.out[0].Text, "modified")
}

func TestCmdEditNotFound(t *testing.T) {
	cmds := &fakeCommandManager{}
	proj := &fakeProj{commands: map[string]projection.Command{}}
	m := Cmd(cmdDeps(proj, cmds))
	cmd := findCmd(t, m, "cmd")

	var col collector
	require.NoError(t, cmd.Run(context.Background(), cmdCtx("bob", "!cmd edit nope New"), "edit nope New", col.emit))

	assert.Empty(t, cmds.upsertCalls)
	require.Len(t, col.out, 1)
	assert.Contains(t, col.out[0].Text, "not found")
	assert.Contains(t, col.out[0].Text, "!cmd add")
}

// --- !cmd remove ---

func TestCmdRemoveSuccess(t *testing.T) {
	cmds := &fakeCommandManager{}
	proj := &fakeProj{commands: map[string]projection.Command{}}
	m := Cmd(cmdDeps(proj, cmds))
	cmd := findCmd(t, m, "cmd")

	var col collector
	require.NoError(t, cmd.Run(context.Background(), cmdCtx("alice", "!cmd remove hello"), "remove hello", col.emit))

	require.Len(t, cmds.deleteCalls, 1)
	assert.Equal(t, "100", cmds.deleteCalls[0].UserID)
	assert.Equal(t, "hello", cmds.deleteCalls[0].Name)

	require.Len(t, col.out, 1)
	assert.Contains(t, col.out[0].Text, "removed")
}

func TestCmdRemoveAcceptsDeleteAlias(t *testing.T) {
	cmds := &fakeCommandManager{}
	proj := &fakeProj{commands: map[string]projection.Command{}}
	m := Cmd(cmdDeps(proj, cmds))
	cmd := findCmd(t, m, "cmd")

	var col collector
	require.NoError(t, cmd.Run(context.Background(), cmdCtx("alice", "!cmd delete test"), "delete test", col.emit))

	require.Len(t, cmds.deleteCalls, 1)
	assert.Equal(t, "test", cmds.deleteCalls[0].Name)
}

// --- error paths ---

// A bare invocation is the public link, not a usage error.
func TestCmdNoSubcommand(t *testing.T) {
	cmds := &fakeCommandManager{}
	proj := &fakeProj{commands: map[string]projection.Command{}}
	m := Cmd(cmdDeps(proj, cmds))
	cmd := findCmd(t, m, "cmd")

	var col collector
	require.NoError(t, cmd.Run(context.Background(), cmdCtx("alice", "!cmd"), "", col.emit))

	require.Len(t, col.out, 1)
	assert.Contains(t, col.out[0].Text, "/user/streamer")
	assert.Empty(t, cmds.upsertCalls)
}

// The URL carries the login only. The display name is chat-facing copy, never
// part of the link: the page used to read its channel label out of a ?channel=
// query, which let anyone edit a shared link and show one channel's commands
// under another streamer's name.
func TestCmdLinkCarriesLoginNotDisplayName(t *testing.T) {
	cmds := &fakeCommandManager{}
	proj := &fakeProj{commands: map[string]projection.Command{}}
	m := Cmd(cmdDeps(proj, cmds))
	cmd := findCmd(t, m, "cmd")

	c := cmdCtx("alice", "!cmd")
	c.Env.BroadcasterUserName = "StreamerName"

	var col collector
	require.NoError(t, cmd.Run(context.Background(), c, "", col.emit))

	require.Len(t, col.out, 1)
	assert.Contains(t, col.out[0].Text, "/user/streamer")
	// The display name still appears in the chat copy ("StreamerName's
	// commands"); what must not carry it is the URL.
	assert.NotContains(t, col.out[0].Text, "/user/StreamerName")
	assert.NotContains(t, col.out[0].Text, "channel=")
}

// An event with no login (non-chat sources can omit it) falls back to the
// broadcaster id, which the page still resolves.
func TestCmdLinkFallsBackToIDWithoutLogin(t *testing.T) {
	cmds := &fakeCommandManager{}
	proj := &fakeProj{commands: map[string]projection.Command{}}
	m := Cmd(cmdDeps(proj, cmds))
	cmd := findCmd(t, m, "cmd")

	c := cmdCtx("alice", "!cmd")
	c.Env.BroadcasterUserLogin = ""

	var col collector
	require.NoError(t, cmd.Run(context.Background(), c, "", col.emit))

	require.Len(t, col.out, 1)
	assert.Contains(t, col.out[0].Text, "/user/100")
}

// An unknown subcommand also falls through to the public link.
func TestCmdInvalidSubcommand(t *testing.T) {
	cmds := &fakeCommandManager{}
	proj := &fakeProj{commands: map[string]projection.Command{}}
	m := Cmd(cmdDeps(proj, cmds))
	cmd := findCmd(t, m, "cmd")

	var col collector
	require.NoError(t, cmd.Run(context.Background(), cmdCtx("alice", "!cmd foobar"), "foobar", col.emit))

	require.Len(t, col.out, 1)
	assert.Contains(t, col.out[0].Text, "/user/streamer")
}

// --- public link + permission gate ---

func TestCmdEveryonePermAndAliases(t *testing.T) {
	m := Cmd(cmdDeps(&fakeProj{}, &fakeCommandManager{}))
	cmd := findCmd(t, m, "cmd")

	assert.Equal(t, module.RoleEveryone, cmd.Perm)
	assert.ElementsMatch(t, []string{"cmds", "command", "commands"}, cmd.Aliases)
}

func TestCmdLinkForViewer(t *testing.T) {
	cmds := &fakeCommandManager{}
	proj := &fakeProj{commands: map[string]projection.Command{}}
	m := Cmd(cmdDeps(proj, cmds))
	cmd := findCmd(t, m, "cmd")

	var col collector
	require.NoError(t, cmd.Run(context.Background(), viewerCtx("vic", "!cmds"), "", col.emit))

	require.Len(t, col.out, 1)
	assert.Contains(t, col.out[0].Text, "@vic")
	assert.Contains(t, col.out[0].Text, "/user/streamer")
	assert.Empty(t, cmds.upsertCalls)
}

// A viewer who tries a management subcommand is denied the mutation and handed
// the link instead.
func TestCmdManageDeniedForViewer(t *testing.T) {
	cmds := &fakeCommandManager{}
	proj := &fakeProj{commands: map[string]projection.Command{}}
	m := Cmd(cmdDeps(proj, cmds))
	cmd := findCmd(t, m, "cmd")

	var col collector
	require.NoError(t, cmd.Run(context.Background(), viewerCtx("vic", "!cmd add hi yo"), "add hi yo", col.emit))

	assert.Empty(t, cmds.upsertCalls, "viewer must not manage commands")
	require.Len(t, col.out, 1)
	assert.Contains(t, col.out[0].Text, "/user/streamer")
}

// The link's origin: a configured PublicBaseURL verbatim minus any trailing
// slash, and otherwise the short commands host the deploy routes to the same
// console app -- not the dashboard host the link used to name.
func TestCmdLinkBase(t *testing.T) {
	cases := []struct {
		name string
		base string
		want string
	}{
		{"configured base", "https://staging.example.com/", "https://staging.example.com/user/streamer"},
		{"unset base", "", "https://commands.itsbagelbot.com/user/streamer"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := cmdDeps(&fakeProj{commands: map[string]projection.Command{}}, &fakeCommandManager{})
			d.PublicBaseURL = tc.base
			cmd := findCmd(t, Cmd(d), "cmd")

			var col collector
			require.NoError(t, cmd.Run(context.Background(), viewerCtx("vic", "!command"), "", col.emit))

			require.Len(t, col.out, 1)
			assert.Contains(t, col.out[0].Text, tc.want)
			assert.NotContains(t, col.out[0].Text, "example.com//user")
		})
	}
}

func TestCmdAddRPCError(t *testing.T) {
	cmds := &fakeCommandManager{upsertErr: errors.New("rpc timeout")}
	proj := &fakeProj{commands: map[string]projection.Command{}}
	m := Cmd(cmdDeps(proj, cmds))
	cmd := findCmd(t, m, "cmd")

	var col collector
	require.NoError(t, cmd.Run(context.Background(), cmdCtx("alice", "!cmd add test Hi"), "add test Hi", col.emit))

	// On RPC error: no reply emitted, error is logged.
	assert.Empty(t, col.out)
}

func TestCmdStripsExclamationFromName(t *testing.T) {
	cmds := &fakeCommandManager{}
	proj := &fakeProj{commands: map[string]projection.Command{}}
	m := Cmd(cmdDeps(proj, cmds))
	cmd := findCmd(t, m, "cmd")

	var col collector
	require.NoError(t, cmd.Run(context.Background(), cmdCtx("alice", "!cmd add !test Hello"), "add !test Hello", col.emit))

	require.Len(t, cmds.upsertCalls, 1)
	assert.Equal(t, "test", cmds.upsertCalls[0].Name, "should strip leading ! from name")
}

// --- splitFirst helper ---

func TestSplitFirst(t *testing.T) {
	tests := []struct {
		input     string
		wantFirst string
		wantRest  string
	}{
		{"add hello world", "add", "hello world"},
		{"remove test", "remove", "test"},
		{"hello", "hello", ""},
		{"  spaces  around  ", "spaces", "around"},
		{"", "", ""},
	}
	for _, tt := range tests {
		first, rest := splitFirst(tt.input)
		assert.Equal(t, tt.wantFirst, first, "splitFirst(%q) first", tt.input)
		assert.Equal(t, tt.wantRest, rest, "splitFirst(%q) rest", tt.input)
	}
}

// --- stream editor ---

func TestStreamEditorCommandShape(t *testing.T) {
	m := Cmd(cmdDeps(&fakeProj{}, &fakeCommandManager{}))
	title := findCmd(t, m, "title")
	assert.Equal(t, module.RoleLeadModerator, title.Perm)
	assert.ElementsMatch(t, []string{"settitle"}, title.Aliases)
	assert.False(t, title.LiveOnly)
	assert.Equal(t, streamEditCooldown, title.Cooldown)

	game := findCmd(t, m, "game")
	assert.Equal(t, module.RoleLeadModerator, game.Perm)
	assert.ElementsMatch(t, []string{"setgame"}, game.Aliases)

	tags := findCmd(t, m, "tags")
	assert.Equal(t, module.RoleLeadModerator, tags.Perm)
	assert.ElementsMatch(t, []string{"settags"}, tags.Aliases)

	commercial := findCmd(t, m, "commercial")
	assert.Equal(t, module.RoleLeadModerator, commercial.Perm)
	assert.ElementsMatch(t, []string{"ad"}, commercial.Aliases)
	assert.True(t, commercial.LiveOnly)
	assert.Equal(t, streamCommercialCooldown, commercial.Cooldown)

	marker := findCmd(t, m, "marker")
	assert.Equal(t, module.RoleLeadModerator, marker.Perm)
	assert.True(t, marker.LiveOnly)
	assert.Equal(t, streamMarkerCooldown, marker.Cooldown)
}

func TestTitleGetEmitsChannelUpdate(t *testing.T) {
	cmd := findCmd(t, Cmd(cmdDeps(&fakeProj{}, &fakeCommandManager{})), "title")
	var col collector
	require.NoError(t, cmd.Run(context.Background(), cmdCtx("alice", "!title"), "", col.emit))
	require.Len(t, col.out, 1)
	o := col.out[0]
	assert.Equal(t, outgress.TypeChannelUpdate, o.Type)
	assert.Equal(t, "100", o.BroadcasterID)
	assert.Equal(t, "title", o.Reason)
	assert.Empty(t, o.Text)
	assert.Equal(t, "alice", o.To)
}

func TestTitleSetEmitsValue(t *testing.T) {
	cmd := findCmd(t, Cmd(cmdDeps(&fakeProj{}, &fakeCommandManager{})), "title")
	var col collector
	require.NoError(t, cmd.Run(context.Background(), cmdCtx("alice", "!title Ranked grind"), "Ranked grind", col.emit))
	require.Len(t, col.out, 1)
	assert.Equal(t, outgress.TypeChannelUpdate, col.out[0].Type)
	assert.Equal(t, "Ranked grind", col.out[0].Text)
	assert.Equal(t, "title", col.out[0].Reason)
}

func TestSettitleEmptyPrintsUsage(t *testing.T) {
	cmd := findCmd(t, Cmd(cmdDeps(&fakeProj{}, &fakeCommandManager{})), "title")
	var col collector
	require.NoError(t, cmd.Run(context.Background(), cmdCtx("alice", "!settitle"), "", col.emit))
	require.Len(t, col.out, 1)
	assert.Equal(t, outgress.TypeChat, col.out[0].Type)
	assert.Contains(t, col.out[0].Text, "Usage")
	assert.Contains(t, col.out[0].Text, "!settitle")
}

func TestTitleTooLongRefuses(t *testing.T) {
	cmd := findCmd(t, Cmd(cmdDeps(&fakeProj{}, &fakeCommandManager{})), "title")
	var col collector
	long := strings.Repeat("a", streamTitleMax+1)
	require.NoError(t, cmd.Run(context.Background(), cmdCtx("alice", "!title "+long), long, col.emit))
	require.Len(t, col.out, 1)
	assert.Equal(t, outgress.TypeChat, col.out[0].Type)
	assert.Contains(t, col.out[0].Text, "too long")
}

func TestTitleSuppressedWhenDisabled(t *testing.T) {
	proj := &fakeProj{modules: []projection.ModuleView{{Name: "title", IsEnabled: false}}}
	cmd := findCmd(t, Cmd(cmdDeps(proj, &fakeCommandManager{})), "title")
	var col collector
	require.NoError(t, cmd.Run(context.Background(), cmdCtx("alice", "!title hello"), "hello", col.emit))
	assert.Empty(t, col.out)
}

func TestCommercialEmitsLength(t *testing.T) {
	cmd := findCmd(t, Cmd(cmdDeps(&fakeProj{}, &fakeCommandManager{})), "commercial")
	var col collector
	require.NoError(t, cmd.Run(context.Background(), cmdCtx("alice", "!commercial 60"), "60", col.emit))
	require.Len(t, col.out, 1)
	assert.Equal(t, outgress.TypeCommercial, col.out[0].Type)
	assert.Equal(t, 60.0, col.out[0].Duration)
	assert.Equal(t, "alice", col.out[0].To)
}

func TestCommercialBareIsThirty(t *testing.T) {
	cmd := findCmd(t, Cmd(cmdDeps(&fakeProj{}, &fakeCommandManager{})), "commercial")
	var col collector
	require.NoError(t, cmd.Run(context.Background(), cmdCtx("alice", "!commercial"), "", col.emit))
	require.Len(t, col.out, 1)
	assert.Equal(t, 30.0, col.out[0].Duration)
}

func TestCommercialBadLengthPrintsUsage(t *testing.T) {
	cmd := findCmd(t, Cmd(cmdDeps(&fakeProj{}, &fakeCommandManager{})), "commercial")
	var col collector
	require.NoError(t, cmd.Run(context.Background(), cmdCtx("alice", "!commercial 45"), "45", col.emit))
	require.Len(t, col.out, 1)
	assert.Equal(t, outgress.TypeChat, col.out[0].Type)
	assert.Contains(t, col.out[0].Text, "Usage")
}

func TestMarkerEmitsDescription(t *testing.T) {
	cmd := findCmd(t, Cmd(cmdDeps(&fakeProj{}, &fakeCommandManager{})), "marker")
	var col collector
	require.NoError(t, cmd.Run(context.Background(), cmdCtx("alice", "!marker Boss fight"), "Boss fight", col.emit))
	require.Len(t, col.out, 1)
	assert.Equal(t, outgress.TypeStreamMarker, col.out[0].Type)
	assert.Equal(t, "Boss fight", col.out[0].Text)
}

func TestParseStreamTags(t *testing.T) {
	got, err := parseStreamTags("English, family friendly")
	require.NoError(t, err)
	assert.Equal(t, []string{"English", "family friendly"}, got)

	_, err = parseStreamTags("")
	assert.Error(t, err)
	_, err = parseStreamTags(strings.Repeat("a", streamTagMaxLen+1))
	assert.Error(t, err)
	tooMany := make([]string, streamTagMaxCount+1)
	for i := range tooMany {
		tooMany[i] = "t"
	}
	_, err = parseStreamTags(strings.Join(tooMany, ","))
	assert.Error(t, err)
}

func TestParseCommercialLength(t *testing.T) {
	n, ok := parseCommercialLength("")
	assert.True(t, ok)
	assert.Equal(t, 30, n)
	n, ok = parseCommercialLength("180")
	assert.True(t, ok)
	assert.Equal(t, 180, n)
	_, ok = parseCommercialLength("45")
	assert.False(t, ok)
	_, ok = parseCommercialLength("nope")
	assert.False(t, ok)
}

func TestStreamIsSetAlias(t *testing.T) {
	assert.True(t, streamIsSetAlias(cmdCtx("alice", "!settitle")))
	assert.True(t, streamIsSetAlias(cmdCtx("alice", "!setgame Fortnite")))
	assert.False(t, streamIsSetAlias(cmdCtx("alice", "!title")))
	assert.False(t, streamIsSetAlias(cmdCtx("alice", "!game")))
}
