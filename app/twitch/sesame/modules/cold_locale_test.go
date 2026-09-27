package modules

import (
	"ItsBagelBot/app/twitch/sesame/engine"
	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/internal/domain/event/lane"
	"context"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestTriggersLoadLocaleOnlyOnMatchAndKeepLocalizedTokens(t *testing.T) {
	calls := 0
	c := &module.Context{Env: lane.Envelope{Text: "ordinary unmatched prose", BroadcasterUserID: "42"}, BroadcasterID: 42, Config: []byte(`{"rules":"hello => {countup:2020-01-01}"}`), LocaleLookup: func(context.Context, uint64) (string, error) { calls++; return "fr", nil }}
	var col collector
	require.NoError(t, triggersOnChat(context.Background(), c, col.emit))
	require.Zero(t, calls)
	c.Env.Text = "hello"
	require.NoError(t, triggersOnChat(context.Background(), c, col.emit))
	require.Equal(t, 1, calls)
	require.Len(t, col.out, 1)
	french := module.KV().WithLocale(module.Locale("fr")).ExpandString("{countup:2020-01-01}")
	english := module.KV().WithLocale(module.Locale("en")).ExpandString("{countup:2020-01-01}")
	require.Equal(t, french, col.out[0].Text)
	require.NotEqual(t, english, col.out[0].Text)
}
func TestPersonalityLoadLocaleOnlyOnMatch(t *testing.T) {
	calls := 0
	c := &module.Context{Env: lane.Envelope{Text: "ordinary unmatched prose", BroadcasterUserID: "42"}, BroadcasterID: 42, LocaleLookup: func(context.Context, uint64) (string, error) { calls++; return "fr", nil }}
	var col collector
	handler := personalityOnChat(engine.Deps{})
	require.NoError(t, handler(context.Background(), c, col.emit))
	require.Zero(t, calls)
	c.Env.Text = "good bagel"
	require.NoError(t, handler(context.Background(), c, col.emit))
	require.Equal(t, 1, calls)
	require.Len(t, col.out, 1)
}
