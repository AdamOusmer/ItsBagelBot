package modules

import (
	"context"
	"testing"

	"ItsBagelBot/app/sesame/engine"
	"ItsBagelBot/app/sesame/module"
	"ItsBagelBot/internal/domain/event/lane"
	"ItsBagelBot/internal/domain/outgress"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func triggersHandler(t *testing.T) module.EventHandler {
	t.Helper()
	m := Triggers(engine.Deps{Log: zap.NewNop()})
	assert.Equal(t, "triggers", m.Name)
	assert.Equal(t, module.KindOptIn, m.Kind)
	h := m.Events["channel.chat.message"]
	require.NotNil(t, h, "triggers must handle channel.chat.message")
	return h
}

func triggersCtx(text, config string) *module.Context {
	c := &module.Context{
		Env: lane.Envelope{
			Type:              "channel.chat.message",
			Text:              text,
			BroadcasterUserID: "2",
			ChatterUserName:   "Bob",
		},
		BroadcasterID: 2,
		Log:           zap.NewNop(),
	}
	if config != "" {
		c.Config = []byte(config)
	}
	return c
}

const helloCfg = `{"triggers":[{"phrase":"hello","response":"hi {user}!"}]}`

func TestTriggersWordMatch(t *testing.T) {
	var col collector
	require.NoError(t, triggersHandler(t)(context.Background(), triggersCtx("oh hello there", helloCfg), col.emit))
	require.Len(t, col.out, 1)
	o := col.out[0]
	assert.Equal(t, outgress.TypeChat, o.Type)
	assert.Equal(t, "2", o.BroadcasterID)
	assert.Equal(t, "hi Bob!", o.Text)
}

func TestTriggersWordMatchIsWholeWord(t *testing.T) {
	var col collector
	// "hello" must not fire on "hellovision" under the default word mode.
	require.NoError(t, triggersHandler(t)(context.Background(), triggersCtx("watch hellovision", helloCfg), col.emit))
	assert.Empty(t, col.out)
}

func TestTriggersCaseInsensitiveByDefault(t *testing.T) {
	var col collector
	require.NoError(t, triggersHandler(t)(context.Background(), triggersCtx("HELLO everyone", helloCfg), col.emit))
	require.Len(t, col.out, 1)
	assert.Equal(t, "hi Bob!", col.out[0].Text)
}

func TestTriggersContainsMode(t *testing.T) {
	var col collector
	cfg := `{"triggers":[{"phrase":"lol","response":"lmao","match":"contains"}]}`
	require.NoError(t, triggersHandler(t)(context.Background(), triggersCtx("hahalolhaha", cfg), col.emit))
	require.Len(t, col.out, 1)
	assert.Equal(t, "lmao", col.out[0].Text)
}

func TestTriggersExactMode(t *testing.T) {
	cfg := `{"triggers":[{"phrase":"gg","response":"good game","match":"exact"}]}`

	var hit collector
	require.NoError(t, triggersHandler(t)(context.Background(), triggersCtx("gg", cfg), hit.emit))
	require.Len(t, hit.out, 1)

	var miss collector
	require.NoError(t, triggersHandler(t)(context.Background(), triggersCtx("gg wp", cfg), miss.emit))
	assert.Empty(t, miss.out)
}

func TestTriggersPrefixMode(t *testing.T) {
	cfg := `{"triggers":[{"phrase":"!hype","response":"HYPE"}, {"phrase":"gm","response":"morning","match":"prefix"}]}`
	// leading "!" lines are skipped entirely, so only the prefix rule can fire.
	var col collector
	require.NoError(t, triggersHandler(t)(context.Background(), triggersCtx("gm chat", cfg), col.emit))
	require.Len(t, col.out, 1)
	assert.Equal(t, "morning", col.out[0].Text)
}

func TestTriggersSkipsCommands(t *testing.T) {
	var col collector
	cfg := `{"triggers":[{"phrase":"hello","response":"hi","match":"contains"}]}`
	require.NoError(t, triggersHandler(t)(context.Background(), triggersCtx("!hello", cfg), col.emit))
	assert.Empty(t, col.out)
}

func TestTriggersSkipsCohorts(t *testing.T) {
	c := triggersCtx("hello", helloCfg)
	c.Env.Senders = []lane.Sender{{ChatterUserID: "9"}}
	var col collector
	require.NoError(t, triggersHandler(t)(context.Background(), c, col.emit))
	assert.Empty(t, col.out)
}

func TestTriggersFirstMatchWins(t *testing.T) {
	cfg := `{"triggers":[{"phrase":"hi","response":"one"},{"phrase":"there","response":"two"}]}`
	var col collector
	require.NoError(t, triggersHandler(t)(context.Background(), triggersCtx("hi there", cfg), col.emit))
	require.Len(t, col.out, 1)
	assert.Equal(t, "one", col.out[0].Text)
}

func TestTriggersNoConfigNoOp(t *testing.T) {
	var col collector
	require.NoError(t, triggersHandler(t)(context.Background(), triggersCtx("hello", ""), col.emit))
	assert.Empty(t, col.out)
}

func TestTriggersEmptyPhraseOrResponseSkipped(t *testing.T) {
	var col collector
	cfg := `{"triggers":[{"phrase":"","response":"x"},{"phrase":"y","response":""},{"phrase":"z","response":"zed"}]}`
	require.NoError(t, triggersHandler(t)(context.Background(), triggersCtx("z", cfg), col.emit))
	require.Len(t, col.out, 1)
	assert.Equal(t, "zed", col.out[0].Text)
}
