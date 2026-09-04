// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"context"
	"errors"
	"testing"

	"ItsBagelBot/app/twitch/sesame/engine"
	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/internal/domain/event/lane"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// stubEmotePlay records every Bump and replays a canned result, so handler
// tests never need valkey.
type stubEmotePlay struct {
	updates []engine.EmotePlayUpdate
	result  engine.EmotePlayResult
	err     error
}

func (s *stubEmotePlay) Bump(_ context.Context, u engine.EmotePlayUpdate) (engine.EmotePlayResult, error) {
	s.updates = append(s.updates, u)
	return s.result, s.err
}

func emotePlayDeps(store *stubEmotePlay) engine.Deps {
	return engine.Deps{Log: zap.NewNop(), EmotePlay: store}
}

func runEmotePlay(t *testing.T, d engine.Deps, env lane.Envelope) []module.Output {
	t.Helper()
	m := EmotePlay(d)
	require.Equal(t, emoteplayModuleName, m.Name)
	assert.Equal(t, module.KindOptIn, m.Kind, "the module speaks unprompted; it must ship disabled")
	handler := m.Events["channel.chat.message"]
	require.NotNil(t, handler)
	var col collector
	c := &module.Context{Env: env, BroadcasterID: 42, Log: zap.NewNop()}
	require.NoError(t, handler(context.Background(), c, col.emit))
	return col.out
}

func TestEmoteShape(t *testing.T) {
	cases := []struct {
		name  string
		text  string
		token string
		width int
		ok    bool
	}{
		{"single emote", "Kappa", "Kappa", 1, true},
		{"pyramid line", "Kappa Kappa Kappa", "Kappa", 3, true},
		{"extra inner spaces", "  Kappa   Kappa  ", "Kappa", 2, true},
		{"prose", "hello there friends", "", 0, false},
		{"mixed emotes", "Kappa PogChamp", "", 0, false},
		{"prefix blend", "Kappa KappaKappa", "", 0, false},
		{"case differs", "Kappa kappa", "", 0, false},
		{"punctuation spam", ". . . .", "", 0, false},
		{"wall over cap", repeatToken("Kappa", maxPyramidWidth+1), "", 0, false},
		{"exactly at cap", repeatToken("Kappa", maxPyramidWidth), "Kappa", maxPyramidWidth, true},
		{"empty", "", "", 0, false},
		{"cjk token", "全員 全員 全員", "全員", 3, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tok, w, ok := emoteShape(tc.text)
			assert.Equal(t, tc.ok, ok)
			assert.Equal(t, tc.token, tok)
			assert.Equal(t, tc.width, w)
		})
	}
}

// repeatToken joins n copies of s with single spaces (test helper).
func repeatToken(s string, n int) string {
	out := s
	for i := 1; i < n; i++ {
		out += " " + s
	}
	return out
}

func TestEmotePlayCandidateFeedsStoreOnce(t *testing.T) {
	store := &stubEmotePlay{}
	runEmotePlay(t, emotePlayDeps(store), lane.Envelope{
		Type: "channel.chat.message", MsgID: "m1", Text: "Kappa Kappa",
	})
	require.Len(t, store.updates, 1)
	u := store.updates[0]
	assert.Equal(t, uint64(42), u.BroadcasterID)
	assert.Equal(t, "m1", u.MsgID)
	assert.Equal(t, "Kappa", u.Emote)
	assert.Equal(t, 2, u.Width)
	assert.Equal(t, 1, u.Copies)
}

func TestEmotePlayProseNeverTouchesStore(t *testing.T) {
	store := &stubEmotePlay{}
	for _, text := range []string{"", "just chatting", "Kappa PogChamp Kappa"} {
		runEmotePlay(t, emotePlayDeps(store), lane.Envelope{Text: text})
	}
	assert.Empty(t, store.updates, "prose must cost zero store round trips")
}

func TestEmotePlayCohortCountsCopies(t *testing.T) {
	store := &stubEmotePlay{}
	line := lane.Sender{ChatterUserID: "2"}
	runEmotePlay(t, emotePlayDeps(store), lane.Envelope{
		Text: "Kappa", Senders: []lane.Sender{line, line, line},
	})
	require.Len(t, store.updates, 1)
	assert.Equal(t, 3, store.updates[0].Copies)
}

func TestEmotePlayAnnouncements(t *testing.T) {
	base := lane.Envelope{Type: "channel.chat.message", BroadcasterUserID: "42", Text: "Kappa"}

	t.Run("pyramid completion announces height", func(t *testing.T) {
		store := &stubEmotePlay{result: engine.EmotePlayResult{PyramidDone: true, Apex: 4}}
		out := runEmotePlay(t, emotePlayDeps(store), base)
		require.Len(t, out, 1)
		assert.Contains(t, out[0].Text, "Kappa")
		assert.Contains(t, out[0].Text, "4")
	})

	t.Run("streak milestone announces rung", func(t *testing.T) {
		store := &stubEmotePlay{result: engine.EmotePlayResult{StreakMilestone: true, Streak: 10}}
		out := runEmotePlay(t, emotePlayDeps(store), base)
		require.Len(t, out, 1)
		assert.Contains(t, out[0].Text, "Kappa")
		assert.Contains(t, out[0].Text, "10")
	})

	t.Run("completion wins over same-line streak rung", func(t *testing.T) {
		store := &stubEmotePlay{result: engine.EmotePlayResult{
			PyramidDone: true, Apex: 3, StreakMilestone: true, Streak: 5}}
		out := runEmotePlay(t, emotePlayDeps(store), base)
		require.Len(t, out, 1, "one line must not be celebrated twice")
		assert.Contains(t, out[0].Text, "pyramid")
	})

	t.Run("silent advance emits nothing", func(t *testing.T) {
		store := &stubEmotePlay{}
		out := runEmotePlay(t, emotePlayDeps(store), base)
		assert.Empty(t, out)
	})
}

func TestEmotePlayStoreErrorFailsOpenSilently(t *testing.T) {
	store := &stubEmotePlay{err: errors.New("valkey down")}
	out := runEmotePlay(t, emotePlayDeps(store), lane.Envelope{Text: "Kappa"})
	assert.Empty(t, out, "an outage must never emit, block or nack chat")
}

func TestEmotePlayNilStoreInert(t *testing.T) {
	out := runEmotePlay(t, engine.Deps{Log: zap.NewNop()}, lane.Envelope{Text: "Kappa"})
	assert.Empty(t, out)
}
