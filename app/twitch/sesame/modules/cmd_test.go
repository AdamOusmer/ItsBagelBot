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

type upsertCall struct{ UserID, Name, Response string }
type deleteCall struct{ UserID, Name string }

func (f *fakeCommandManager) Upsert(_ context.Context, userID, name, response string) error {
	f.upserts = append(f.upserts, upsertCall{userID, name, response})
	return f.upsertErr
}

func (f *fakeCommandManager) Delete(_ context.Context, userID, name string) error {
	f.deletes = append(f.deletes, deleteCall{userID, name})
	return f.deleteErr
}

func cmdDeps(proj projection.Reader, cmds engine.CommandManager) engine.Deps {
	return engine.Deps{Proj: proj, Commands: cmds, Log: zap.NewNop()}
}

func existingCommands(names ...string) map[string]projection.Command {
	cmds := make(map[string]projection.Command, len(names))
	for _, name := range names {
		cmds[name] = projection.Command{Name: name, Response: "Hi!"}
	}
	return cmds
}

func TestCmdManagement(t *testing.T) {
	cases := []struct {
		name      string
		text      string
		viewer    bool
		existing  []string
		upsertErr error
		deleteErr error
		upserts   []upsertCall
		deletes   []deleteCall
		contains  []string
	}{
		{name: "adds a command and confirms it", text: "!cmd add hello Hello world!",
			upserts: []upsertCall{{"100", "hello", "Hello world!"}}, contains: []string{"@alice", "hello", "added"}},
		{name: "strips the leading bang from the name", text: "!cmd add !test Hello",
			upserts: []upsertCall{{"100", "test", "Hello"}}, contains: []string{"test", "added"}},
		{name: "refuses to overwrite an existing command", text: "!cmd add hello New", existing: []string{"hello"},
			contains: []string{"already exists", "!cmd edit"}},
		{name: "asks for a response when add has none", text: "!cmd add hello", contains: []string{"response"}},
		{name: "prints usage when add has no name", text: "!cmd add", contains: []string{"Usage"}},
		{name: "edits an existing command", text: "!cmd edit hello Updated!", existing: []string{"hello"},
			upserts: []upsertCall{{"100", "hello", "Updated!"}}, contains: []string{"modified"}},
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
			upserts: []upsertCall{{"100", "test", "Hi"}}},
		{name: "stays silent when the remove RPC fails", text: "!cmd remove test", deleteErr: errors.New("rpc timeout"),
			deletes: []deleteCall{{"100", "test"}}},
		{name: "viewers get the page link instead of managing", text: "!cmd add hi yo", viewer: true,
			contains: []string{"/user/streamer"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cmds := &fakeCommandManager{upsertErr: tc.upsertErr, deleteErr: tc.deleteErr}
			m := Cmd(cmdDeps(&fakeProj{commands: existingCommands(tc.existing...)}, cmds))
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
		{name: "link carries the login, not the display name", text: "!cmd", displayName: "StreamerName",
			contains: []string{"/user/streamer"}, excludes: []string{"/user/StreamerName", "channel="}},
		{name: "falls back to the broadcaster id without a login", text: "!cmd", noLogin: true,
			contains: []string{"/user/100"}},
		{name: "uses the configured base URL", text: "!command", baseURL: "https://staging.example.com/",
			contains: []string{"https://staging.example.com/user/streamer"}, excludes: []string{"example.com//user"}},
		{name: "defaults the base URL when unset", text: "!command",
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
			m := Cmd(cmdDeps(&fakeProj{}, &fakeCommandManager{}))
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
			m := Cmd(cmdDeps(&fakeProj{}, &fakeCommandManager{}))
			out := runChat(t, m, chatCtx("42", "alice", "moderator"), tc.text)
			require.Len(t, out, 1)
			assert.Equal(t, outgress.TypeChat, out[0].Type)
			assert.Contains(t, out[0].Text, tc.want)
		})
	}
}

func TestTitleTooLongExpandsNamespacedTokens(t *testing.T) {
	m := Cmd(cmdDeps(&fakeProj{}, &fakeCommandManager{}))
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
			out := runChat(t, Cmd(cmdDeps(proj, &fakeCommandManager{})), chatCtx("42", "alice", "moderator"), "!"+name+" 60")
			assert.Empty(t, out)
		})
	}
}
