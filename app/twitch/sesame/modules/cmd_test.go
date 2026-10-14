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
	"ItsBagelBot/internal/domain/i18n"
	"ItsBagelBot/internal/domain/outgress"
	"ItsBagelBot/internal/projection"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type fakeCommandManager struct {
	upserts   []upsertCall
	deletes   []deleteCall
	upsertErr error
	deleteErr error
}

type upsertCall struct {
	UserID string
	Cmd    projection.Command
}
type deleteCall struct{ UserID, Name string }

func (f *fakeCommandManager) Upsert(_ context.Context, userID string, cmd projection.Command) error {
	f.upserts = append(f.upserts, upsertCall{userID, cmd})
	return f.upsertErr
}

func (f *fakeCommandManager) Delete(_ context.Context, userID, name string) error {
	f.deletes = append(f.deletes, deleteCall{userID, name})
	return f.deleteErr
}

func cmdDeps(proj projection.Reader, cmds engine.CommandManager) engine.Deps {
	return engine.Deps{Proj: proj, Commands: cmds, Log: zap.NewNop()}
}

func storedCommand(name string, active bool) projection.Command {
	return projection.Command{Name: name, Aliases: []string{"hey"}, Response: "Hi!", IsActive: active, Perm: "vip", Cooldown: 30, UserCooldown: 5, AllowedUserID: "77", BumpCounter: "hits"}
}

func existingCommands(names ...string) map[string]projection.Command {
	cmds := make(map[string]projection.Command, len(names))
	for _, name := range names {
		cmds[name] = storedCommand(name, true)
	}
	return cmds
}

func storedWith(name string, change func(*projection.Command)) projection.Command {
	cc := storedCommand(name, true)
	change(&cc)
	return cc
}

func newCommand(name, response string) projection.Command {
	return projection.Command{Name: name, Response: response, IsActive: true, Perm: "everyone"}
}

type cmdCase struct {
	name      string
	text      string
	viewer    bool
	existing  []string
	disabled  []string
	lookupErr error
	upsertErr error
	deleteErr error
	upserts   []upsertCall
	deletes   []deleteCall
	contains  []string
}

func runCmdCases(t *testing.T, cases []cmdCase) {
	t.Helper()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stored := existingCommands(tc.existing...)
			for _, name := range tc.disabled {
				stored[name] = storedCommand(name, false)
			}
			cmds := &fakeCommandManager{upsertErr: tc.upsertErr, deleteErr: tc.deleteErr}
			m := Cmd(cmdDeps(&fakeProj{commands: stored, cmdErr: tc.lookupErr}, cmds))
			c := chatCtx("42", "alice", "moderator")
			if tc.viewer {
				c = chatCtx("42", "alice")
			}
			out := runChat(t, m, c, tc.text)
			assert.Equal(t, tc.upserts, cmds.upserts)
			assert.Equal(t, tc.deletes, cmds.deletes)
			require.Len(t, out, min(len(tc.contains), 1))
			for _, want := range tc.contains {
				assert.Contains(t, out[0].Text, want)
			}
		})
	}
}

func TestCmdManagement(t *testing.T) {
	runCmdCases(t, []cmdCase{
		{name: "adds a command and confirms it", text: "!cmd add hello Hello world!",
			upserts: []upsertCall{{"100", newCommand("hello", "Hello world!")}}, contains: []string{"@alice", "hello", "added"}},
		{name: "strips the leading bang from the name", text: "!cmd add !test Hello",
			upserts: []upsertCall{{"100", newCommand("test", "Hello")}}, contains: []string{"test", "added"}},
		{name: "refuses to overwrite an existing command", text: "!cmd add hello New", existing: []string{"hello"},
			contains: []string{"already exists", "!cmd edit"}},
		{name: "asks for a response when add has none", text: "!cmd add hello", contains: []string{"response"}},
		{name: "prints usage when add has no name", text: "!cmd add", contains: []string{"Usage"}},
		{name: "edits the response and keeps every dashboard setting", text: "!cmd edit hello Updated!", existing: []string{"hello"},
			upserts:  []upsertCall{{"100", storedWith("hello", func(c *projection.Command) { c.Response = "Updated!" })}},
			contains: []string{"modified"}},
		{name: "edit keeps a disabled command disabled", text: "!cmd edit hello Updated!", disabled: []string{"hello"},
			upserts:  []upsertCall{{"100", storedWith("hello", func(c *projection.Command) { c.Response, c.IsActive = "Updated!", false })}},
			contains: []string{"modified"}},
		{name: "refuses to edit an unknown command", text: "!cmd edit nope New",
			contains: []string{"not found", "!cmd add"}},
		{name: "asks for a response when edit has none", text: "!cmd edit hello", contains: []string{"response"}},
		{name: "prints usage when edit has no name", text: "!cmd edit", contains: []string{"Usage"}},
		{name: "removes a command", text: "!cmd remove hello",
			deletes: []deleteCall{{"100", "hello"}}, contains: []string{"removed"}},
		{name: "accepts delete as an alias of remove", text: "!cmd delete test",
			deletes: []deleteCall{{"100", "test"}}, contains: []string{"removed"}},
		{name: "prints usage when remove has no name", text: "!cmd remove", contains: []string{"Usage"}},
		{name: "stays silent when the add RPC fails", text: "!cmd add test Hi", upsertErr: errors.New("rpc timeout"),
			upserts: []upsertCall{{"100", newCommand("test", "Hi")}}},
		{name: "stays silent when the remove RPC fails", text: "!cmd remove test", deleteErr: errors.New("rpc timeout"),
			deletes: []deleteCall{{"100", "test"}}},
		{name: "add stays silent and writes nothing when the lookup fails", text: "!cmd add hello Hi",
			lookupErr: errors.New("projection unavailable")},
		{name: "edit stays silent and writes nothing when the lookup fails", text: "!cmd edit hello Hi",
			lookupErr: errors.New("projection unavailable")},
		{name: "viewers get the page link instead of managing", text: "!cmd add hi yo", viewer: true,
			contains: []string{"/user/streamer"}},
	})
}

func TestCmdToggleAndPerm(t *testing.T) {
	runCmdCases(t, []cmdCase{
		{name: "disables a command and keeps its response and settings", text: "!cmd disable !Hello", existing: []string{"hello"},
			upserts:  []upsertCall{{"100", storedCommand("hello", false)}},
			contains: []string{"@alice", "hello", "disabled"}},
		{name: "enables a disabled command", text: "!cmd enable hello", disabled: []string{"hello"},
			upserts:  []upsertCall{{"100", storedCommand("hello", true)}},
			contains: []string{"@alice", "hello", "enabled"}},
		{name: "refuses to disable an unknown command", text: "!cmd disable nope",
			contains: []string{"not found"}},
		{name: "prints usage when disable has no name", text: "!cmd disable", contains: []string{"Usage", "disable"}},
		{name: "disable stays silent and writes nothing when the lookup fails", text: "!cmd disable hello",
			lookupErr: errors.New("projection unavailable")},
		{name: "sets the access level of a command", text: "!cmd perm hello VIP", existing: []string{"hello"},
			upserts:  []upsertCall{{"100", storedWith("hello", func(c *projection.Command) { c.Perm = "vip" })}},
			contains: []string{"hello", "vip"}},
		{name: "lowers a command to everyone", text: "!cmd perm hello everyone", existing: []string{"hello"},
			upserts:  []upsertCall{{"100", storedWith("hello", func(c *projection.Command) { c.Perm = "everyone" })}},
			contains: []string{"hello", "everyone"}},
		{name: "refuses an unknown access level", text: "!cmd perm hello admins", existing: []string{"hello"},
			contains: []string{"Usage", "lead_mod"}},
		{name: "prints perm usage without a level", text: "!cmd perm hello", contains: []string{"Usage", "!cmd perm"}},
		{name: "refuses to set access on an unknown command", text: "!cmd perm nope mod",
			contains: []string{"not found"}},
		{name: "stays silent when the disable RPC fails", text: "!cmd disable hello", existing: []string{"hello"},
			upsertErr: errors.New("rpc timeout"), upserts: []upsertCall{{"100", storedCommand("hello", false)}}},
		{name: "viewers get the page link instead of disabling", text: "!cmd disable hello", viewer: true, existing: []string{"hello"},
			contains: []string{"/user/streamer"}},
	})
}

func TestCmdSubcommandRoles(t *testing.T) {
	cases := []struct {
		name    string
		config  string
		badges  []string
		text    string
		manages bool
	}{
		{name: "mutating subcommands default to moderators", badges: []string{"moderator"}, text: "!cmd disable hello", manages: true},
		{name: "vips cannot manage by default", badges: []string{"vip"}, text: "!cmd disable hello"},
		{name: "everyone is never a management level", config: `{"addPerm":"everyone"}`, text: "!cmd add fresh Hi"},
		{name: "everyone falls back to moderators", config: `{"addPerm":"everyone"}`, badges: []string{"moderator"}, text: "!cmd add fresh Hi", manages: true},
		{name: "a lowered add level lets subscribers add", config: `{"addPerm":"sub"}`, badges: []string{"subscriber"}, text: "!cmd add fresh Hi", manages: true},
		{name: "a lowered level only covers its own subcommand", config: `{"addPerm":"sub"}`, badges: []string{"subscriber"}, text: "!cmd remove hello"},
		{name: "a raised disable level shuts out moderators", config: `{"disablePerm":"broadcaster"}`, badges: []string{"moderator"}, text: "!cmd disable hello"},
		{name: "the broadcaster passes a raised level", config: `{"disablePerm":"broadcaster"}`, badges: []string{"broadcaster"}, text: "!cmd disable hello", manages: true},
		{name: "delete follows the remove level", config: `{"removePerm":"vip"}`, badges: []string{"vip"}, text: "!cmd delete hello", manages: true},
		{name: "enable has its own level", config: `{"enablePerm":"sub"}`, badges: []string{"subscriber"}, text: "!cmd enable hello", manages: true},
		{name: "perm has its own level", config: `{"permPerm":"lead_mod"}`, badges: []string{"moderator"}, text: "!cmd perm hello sub"},
		{name: "an unknown level falls back to moderators", config: `{"editPerm":"everybody"}`, text: "!cmd edit hello Hi"},
		{name: "an unknown level still admits moderators", config: `{"editPerm":"everybody"}`, badges: []string{"moderator"}, text: "!cmd edit hello Hi", manages: true},
		{name: "a non string level falls back to moderators", config: `{"addPerm":1}`, text: "!cmd add fresh Hi"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cmds := &fakeCommandManager{}
			m := Cmd(cmdDeps(&fakeProj{commands: existingCommands("hello")}, cmds))
			out := runChat(t, m, withConfig(chatCtx("42", "alice", tc.badges...), tc.config), tc.text)
			require.Len(t, out, 1)
			assert.Equal(t, tc.manages, len(cmds.upserts)+len(cmds.deletes) == 1)
			assert.Equal(t, !tc.manages, strings.Contains(out[0].Text, "/user/streamer"))
		})
	}
}

func TestCmdLink(t *testing.T) {
	cases := []struct {
		name        string
		text        string
		baseURL     string
		hidden      bool
		projErr     error
		noLogin     bool
		displayName string
		contains    []string
		excludes    []string
	}{
		{name: "links the page by broadcaster login", text: "!cmd", contains: []string{"@alice", "/user/streamer"}},
		{name: "answers the cmds alias", text: "!cmds", contains: []string{"/user/streamer"}},
		{name: "unknown subcommand falls back to the link", text: "!cmd foobar", contains: []string{"/user/streamer"}},
		{name: "TestCmdLinkCarriesLoginNotDisplayName", text: "!cmd", displayName: "StreamerName",
			contains: []string{"/user/streamer"}, excludes: []string{"/user/StreamerName", "channel="}},
		{name: "TestCmdLinkFallsBackToIDWithoutLogin", text: "!cmd", noLogin: true,
			contains: []string{"/user/100"}},
		{name: "TestCmdLinkBase configured base", text: "!command", baseURL: "https://staging.example.com/",
			contains: []string{"https://staging.example.com/user/streamer"}, excludes: []string{"example.com//user"}},
		{name: "TestCmdLinkBase unset base", text: "!command",
			contains: []string{"https://commands.itsbagelbot.com/user/streamer"}},
		{name: "fails open when the projection errors", text: "!cmd", projErr: errors.New("projection unavailable"),
			contains: []string{"/user/streamer"}},
		{name: "hidden page points to no link", text: "!cmd", hidden: true, excludes: []string{"http"},
			contains: []string{strings.NewReplacer("{user}", "alice", "{channel}", "streamer").Replace(i18n.T("", "cmd.page_off"))}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			proj := &fakeProj{user: projection.User{CommandsPageHidden: tc.hidden}, userErr: tc.projErr}
			d := cmdDeps(proj, &fakeCommandManager{})
			d.PublicBaseURL = tc.baseURL
			c := chatCtx("42", "alice")
			c.Env.BroadcasterUserName = tc.displayName
			if tc.noLogin {
				c.Env.BroadcasterUserLogin = ""
			}
			out := runChat(t, Cmd(d), c, tc.text)
			require.Len(t, out, 1)
			for _, want := range tc.contains {
				assert.Contains(t, out[0].Text, want)
			}
			for _, bad := range tc.excludes {
				assert.NotContains(t, out[0].Text, bad)
			}
		})
	}
}

func TestStreamEditorsEmitUpdates(t *testing.T) {
	cases := []struct {
		name string
		text string
		want module.Output
	}{
		{"title without a value reads the current title", "!title",
			module.Output{Type: outgress.TypeChannelUpdate, Reason: "title"}},
		{"title with a value sets it", "!title Ranked grind",
			module.Output{Type: outgress.TypeChannelUpdate, Reason: "title", Text: "Ranked grind"}},
		{"settitle alias sets the title", "!settitle Ranked grind",
			module.Output{Type: outgress.TypeChannelUpdate, Reason: "title", Text: "Ranked grind"}},
		{"a title at the length limit is accepted", "!title " + strings.Repeat("a", streamTitleMax),
			module.Output{Type: outgress.TypeChannelUpdate, Reason: "title", Text: strings.Repeat("a", streamTitleMax)}},
		{"game sets the category", "!game Fortnite",
			module.Output{Type: outgress.TypeChannelUpdate, Reason: "game", Text: "Fortnite"}},
		{"tags set a comma list", "!tags English, family friendly",
			module.Output{Type: outgress.TypeChannelUpdate, Reason: "tags", Text: "English, family friendly"}},
		{"tags at the count limit are accepted", "!tags " + strings.Repeat("t,", streamTagMaxCount-1) + "t",
			module.Output{Type: outgress.TypeChannelUpdate, Reason: "tags", Text: strings.Repeat("t,", streamTagMaxCount-1) + "t"}},
		{"a tag at the length limit is accepted", "!tags " + strings.Repeat("a", streamTagMaxLen),
			module.Output{Type: outgress.TypeChannelUpdate, Reason: "tags", Text: strings.Repeat("a", streamTagMaxLen)}},
		{"commercial runs the requested length", "!commercial 60",
			module.Output{Type: outgress.TypeCommercial, Duration: 60}},
		{"bare commercial runs thirty seconds", "!commercial",
			module.Output{Type: outgress.TypeCommercial, Duration: 30}},
		{"commercial accepts the longest break", "!ad 180",
			module.Output{Type: outgress.TypeCommercial, Duration: 180}},
		{"marker carries the description", "!marker Boss fight",
			module.Output{Type: outgress.TypeStreamMarker, Text: "Boss fight"}},
		{"marker truncates an oversized description", "!marker " + strings.Repeat("a", streamTitleMax+10),
			module.Output{Type: outgress.TypeStreamMarker, Text: strings.Repeat("a", streamTitleMax)}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := StreamEditors(cmdDeps(&fakeProj{}, &fakeCommandManager{}))
			want := tc.want
			want.BroadcasterID, want.To = "100", "alice"
			assert.Equal(t, []module.Output{want}, runChat(t, m, chatCtx("42", "alice", "moderator"), tc.text))
		})
	}
}

func TestStreamEditorsRefuseBadInput(t *testing.T) {
	longTitle := strings.Repeat("a", streamTitleMax+1)
	cases := []struct {
		name, text, want string
	}{
		{"settitle without a value prints usage", "!settitle", "!settitle"},
		{"setgame without a value prints usage", "!setgame", "Usage"},
		{"title over the limit is refused", "!title " + longTitle, "too long"},
		{"tags over the count limit print usage", "!tags " + strings.Repeat("t,", streamTagMaxCount+1), "Usage"},
		{"a tag over the length limit prints usage", "!tags " + strings.Repeat("a", streamTagMaxLen+1), "Usage"},
		{"commercial off the thirty second grid prints usage", "!commercial 45", "Usage"},
		{"non numeric commercial length prints usage", "!commercial nope", "Usage"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := StreamEditors(cmdDeps(&fakeProj{}, &fakeCommandManager{}))
			out := runChat(t, m, chatCtx("42", "alice", "moderator"), tc.text)
			require.Len(t, out, 1)
			assert.Equal(t, outgress.TypeChat, out[0].Type)
			assert.Contains(t, out[0].Text, tc.want)
		})
	}
}

func TestTitleTooLongExpandsNamespacedTokens(t *testing.T) {
	m := StreamEditors(cmdDeps(&fakeProj{}, &fakeCommandManager{}))
	for _, locale := range []string{"en", "fr"} {
		c := chatCtx("42", "alice", "moderator")
		c.Locale = locale
		out := runChat(t, m, c, "!title "+strings.Repeat("a", streamTitleMax+1))
		require.Len(t, out, 1)
		assert.Contains(t, out[0].Text, "@alice", locale)
		assert.NotContains(t, out[0].Text, "{", locale)
	}
}

func TestStreamEditorsSilentWhenModuleDisabled(t *testing.T) {
	for _, name := range []string{"title", "game", "tags", "commercial", "marker"} {
		t.Run(name, func(t *testing.T) {
			proj := &fakeProj{modules: []projection.ModuleView{{Name: name, IsEnabled: false}}}
			out := runChat(t, StreamEditors(cmdDeps(proj, &fakeCommandManager{})), chatCtx("42", "alice", "moderator"), "!"+name+" 60")
			assert.Empty(t, out)
		})
	}
}
