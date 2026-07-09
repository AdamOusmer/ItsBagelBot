package modules

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
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

// triggersCtx builds a chat Context whose Config blob carries the given rules
// textarea (empty rules leaves the blob unset).
func triggersCtx(text, rules string) *module.Context {
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
	if rules != "" {
		c.Config = rulesBlob(rules)
	}
	return c
}

// rulesBlob marshals a rules string into the {"rules":"…"} Configs blob, matching
// what the dashboard persists. Marshaling a single string field never errors.
func rulesBlob(rules string) []byte {
	b, _ := json.Marshal(triggersConfig{Rules: rules})
	return b
}

func TestTriggersWordMatch(t *testing.T) {
	var col collector
	require.NoError(t, triggersHandler(t)(context.Background(), triggersCtx("oh hello there", "hello => hi {user}!"), col.emit))
	require.Len(t, col.out, 1)
	o := col.out[0]
	assert.Equal(t, outgress.TypeChat, o.Type)
	assert.Equal(t, "2", o.BroadcasterID)
	assert.Equal(t, "hi Bob!", o.Text)
}

func TestTriggersWordMatchIsWholeWord(t *testing.T) {
	var col collector
	// "hello" must not fire on "hellovision" under the default word mode.
	require.NoError(t, triggersHandler(t)(context.Background(), triggersCtx("watch hellovision", "hello => hi"), col.emit))
	assert.Empty(t, col.out)
}

func TestTriggersCaseInsensitive(t *testing.T) {
	var col collector
	require.NoError(t, triggersHandler(t)(context.Background(), triggersCtx("HELLO everyone", "hello => hi {user}!"), col.emit))
	require.Len(t, col.out, 1)
	assert.Equal(t, "hi Bob!", col.out[0].Text)
}

func TestTriggersContainsMode(t *testing.T) {
	var col collector
	require.NoError(t, triggersHandler(t)(context.Background(), triggersCtx("hahalolhaha", "contains: lol => lmao"), col.emit))
	require.Len(t, col.out, 1)
	assert.Equal(t, "lmao", col.out[0].Text)
}

func TestTriggersExactMode(t *testing.T) {
	rules := "exact: gg => good game"

	var hit collector
	require.NoError(t, triggersHandler(t)(context.Background(), triggersCtx("gg", rules), hit.emit))
	require.Len(t, hit.out, 1)
	assert.Equal(t, "good game", hit.out[0].Text)

	var miss collector
	require.NoError(t, triggersHandler(t)(context.Background(), triggersCtx("gg wp", rules), miss.emit))
	assert.Empty(t, miss.out)
}

func TestTriggersPrefixMode(t *testing.T) {
	var col collector
	require.NoError(t, triggersHandler(t)(context.Background(), triggersCtx("gm chat", "prefix: gm => morning"), col.emit))
	require.Len(t, col.out, 1)
	assert.Equal(t, "morning", col.out[0].Text)
}

func TestTriggersSkipsCommands(t *testing.T) {
	var col collector
	require.NoError(t, triggersHandler(t)(context.Background(), triggersCtx("!hello", "contains: hello => hi"), col.emit))
	assert.Empty(t, col.out)
}

func TestTriggersSkipsCohorts(t *testing.T) {
	c := triggersCtx("hello", "hello => hi")
	c.Env.Senders = []lane.Sender{{ChatterUserID: "9"}}
	var col collector
	require.NoError(t, triggersHandler(t)(context.Background(), c, col.emit))
	assert.Empty(t, col.out)
}

func TestTriggersFirstMatchWins(t *testing.T) {
	var col collector
	require.NoError(t, triggersHandler(t)(context.Background(), triggersCtx("hi there", "hi => one\nthere => two"), col.emit))
	require.Len(t, col.out, 1)
	assert.Equal(t, "one", col.out[0].Text)
}

func TestTriggersNoConfigNoOp(t *testing.T) {
	var col collector
	require.NoError(t, triggersHandler(t)(context.Background(), triggersCtx("hello", ""), col.emit))
	assert.Empty(t, col.out)
}

// TestParseRules exercises the textarea parser directly: comments, blanks, mode
// prefixes, and malformed lines are all handled.
func TestParseRules(t *testing.T) {
	raw := "# a comment\n\nhello => hi {user}!\n  contains: lol =>  lmao \nnoseparator here\nempty => \n => noPhrase"
	rules := triggersConfig{Rules: raw}.rules()
	require.Len(t, rules, 2)

	assert.Equal(t, "hello", rules[0].Phrase)
	assert.Equal(t, "hi {user}!", rules[0].Response)
	assert.Equal(t, "word", rules[0].Match)

	assert.Equal(t, "lol", rules[1].Phrase)
	assert.Equal(t, "lmao", rules[1].Response)
	assert.Equal(t, "contains", rules[1].Match)
}

// TestParseRulesUnknownMode keeps a colon that is not a real mode as part of the
// phrase rather than dropping it.
func TestParseRulesUnknownMode(t *testing.T) {
	rules := triggersConfig{Rules: "time:30 => later"}.rules()
	require.Len(t, rules, 1)
	assert.Equal(t, "time:30", rules[0].Phrase)
	assert.Equal(t, "word", rules[0].Match)
}

// TestParseRulesCap stops at maxTriggers.
func TestParseRulesCap(t *testing.T) {
	var b strings.Builder
	for i := 0; i < maxTriggers+10; i++ {
		fmt.Fprintf(&b, "w%d => r%d\n", i, i)
	}
	assert.Len(t, triggersConfig{Rules: b.String()}.rules(), maxTriggers)
}
