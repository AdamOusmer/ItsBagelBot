package modules

import (
	"context"
	"errors"
	"testing"

	"ItsBagelBot/app/sesame/engine"
	"ItsBagelBot/app/sesame/module"
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
}

func (f *fakeProj) User(context.Context, uint64) (projection.User, error) {
	return projection.User{}, nil
}

func (f *fakeProj) Modules(context.Context, uint64) ([]projection.ModuleView, error) {
	return nil, nil
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

func cmdCtx(chatterLogin, text string) *module.Context {
	return &module.Context{
		Env: lane.Envelope{
			Type:                 "channel.chat.message",
			BroadcasterUserID:    "100",
			BroadcasterUserLogin: "streamer",
			ChatterUserID:        "42",
			ChatterUserLogin:     chatterLogin,
			Text:                 text,
		},
		Regress:       module.RegressPremium,
		BroadcasterID: 100,
		Log:           zap.NewNop(),
	}
}

// --- !cmd add ---

func TestCmdAddSuccess(t *testing.T) {
	cmds := &fakeCommandManager{}
	proj := &fakeProj{commands: map[string]projection.Command{}}
	m := Cmd(cmdDeps(proj, cmds))
	cmd := findCmd(t, m, "cmd")

	assert.Equal(t, module.RoleModerator, cmd.Perm)

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

func TestCmdNoSubcommand(t *testing.T) {
	cmds := &fakeCommandManager{}
	proj := &fakeProj{commands: map[string]projection.Command{}}
	m := Cmd(cmdDeps(proj, cmds))
	cmd := findCmd(t, m, "cmd")

	var col collector
	require.NoError(t, cmd.Run(context.Background(), cmdCtx("alice", "!cmd"), "", col.emit))

	require.Len(t, col.out, 1)
	assert.Contains(t, col.out[0].Text, "Usage")
}

func TestCmdInvalidSubcommand(t *testing.T) {
	cmds := &fakeCommandManager{}
	proj := &fakeProj{commands: map[string]projection.Command{}}
	m := Cmd(cmdDeps(proj, cmds))
	cmd := findCmd(t, m, "cmd")

	var col collector
	require.NoError(t, cmd.Run(context.Background(), cmdCtx("alice", "!cmd foobar"), "foobar", col.emit))

	require.Len(t, col.out, 1)
	assert.Contains(t, col.out[0].Text, "Usage")
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
		input        string
		wantFirst    string
		wantRest     string
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
