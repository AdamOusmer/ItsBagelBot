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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

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

func emoteLine(emote string, n int) string {
	return strings.TrimSpace(strings.Repeat(emote+" ", n))
}

func TestEmotePlayChat(t *testing.T) {
	kappa := engine.EmotePlayUpdate{BroadcasterID: 42, MsgID: "m1", Emote: "Kappa", Width: 1, Copies: 1}
	line := lane.Sender{ChatterUserID: "2"}
	cases := []struct {
		name     string
		text     string
		senders  []lane.Sender
		result   engine.EmotePlayResult
		err      error
		noStore  bool
		updates  []engine.EmotePlayUpdate
		contains []string
		excludes []string
	}{
		{name: "a single emote is a line of width one", text: "Kappa", updates: []engine.EmotePlayUpdate{kappa}},
		{name: "a pyramid line counts its width", text: "Kappa Kappa Kappa", updates: []engine.EmotePlayUpdate{{BroadcasterID: 42, MsgID: "m1", Emote: "Kappa", Width: 3, Copies: 1}}},
		{name: "extra inner spaces are ignored", text: "  Kappa   Kappa  ", updates: []engine.EmotePlayUpdate{{BroadcasterID: 42, MsgID: "m1", Emote: "Kappa", Width: 2, Copies: 1}}},
		{name: "a line exactly at the width cap counts", text: emoteLine("Kappa", maxPyramidWidth),
			updates: []engine.EmotePlayUpdate{{BroadcasterID: 42, MsgID: "m1", Emote: "Kappa", Width: maxPyramidWidth, Copies: 1}}},
		{name: "a non latin emote token counts", text: "全員 全員 全員", updates: []engine.EmotePlayUpdate{{BroadcasterID: 42, MsgID: "m1", Emote: "全員", Width: 3, Copies: 1}}},
		{name: "a wall over the cap never touches the store", text: emoteLine("Kappa", maxPyramidWidth+1)},
		{name: "a prefix blend never touches the store", text: "Kappa KappaKappa"},
		{name: "a case difference never touches the store", text: "Kappa kappa"},
		{name: "punctuation spam never touches the store", text: ". . . ."},
		{name: "empty text never touches the store", text: ""},
		{name: "whitespace only never touches the store", text: "   "},
		{name: "prose never touches the store", text: "just chatting"},
		{name: "mixed emotes never touch the store", text: "Kappa PogChamp Kappa"},
		{name: "a folded cohort counts its copies", text: "Kappa", senders: []lane.Sender{line, line, line},
			updates: []engine.EmotePlayUpdate{{BroadcasterID: 42, MsgID: "m1", Emote: "Kappa", Width: 1, Copies: 3}}},
		{name: "a finished pyramid announces its height", text: "Kappa", result: engine.EmotePlayResult{PyramidDone: true, Apex: 4},
			updates: []engine.EmotePlayUpdate{kappa}, contains: []string{"Kappa", "4"}},
		{name: "a streak milestone announces the rung", text: "Kappa", result: engine.EmotePlayResult{StreakMilestone: true, Streak: 10},
			updates: []engine.EmotePlayUpdate{kappa}, contains: []string{"Kappa", "10"}},
		{name: "completion wins over a same line streak rung", text: "Kappa", result: engine.EmotePlayResult{PyramidDone: true, Apex: 3, StreakMilestone: true, Streak: 5},
			updates: []engine.EmotePlayUpdate{kappa}, contains: []string{"pyramid"}},
		{name: "a silent advance emits nothing", text: "Kappa", updates: []engine.EmotePlayUpdate{kappa}},
		{name: "a store outage fails open silently", text: "Kappa", err: errors.New("valkey down"), updates: []engine.EmotePlayUpdate{kappa}},
		{name: "a missing store keeps the module inert", text: "Kappa", noStore: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := &stubEmotePlay{result: tc.result, err: tc.err}
			d := emotePlayDeps(store)
			if tc.noStore {
				d.EmotePlay = nil
			}
			c := &module.Context{
				Env:           lane.Envelope{Type: "channel.chat.message", MsgID: "m1", Text: tc.text, Senders: tc.senders},
				BroadcasterID: 42,
				Log:           zap.NewNop(),
			}
			out := runEvent(t, EmotePlay(d), c)
			assert.Equal(t, tc.updates, store.updates)
			if tc.contains == nil {
				assert.Empty(t, out)
				return
			}
			require.Len(t, out, 1, "one line must not be celebrated twice")
			assertText(t, out[0].Text, textWant{"", tc.contains, tc.excludes})
		})
	}
}

func TestEmotePlayResolvesLocaleOnlyForMilestones(t *testing.T) {
	for _, milestone := range []bool{false, true} {
		name := "no milestone"
		if milestone {
			name = "milestone"
		}
		t.Run(name, func(t *testing.T) {
			store := &stubEmotePlay{result: engine.EmotePlayResult{StreakMilestone: milestone, Streak: 10}}
			calls := 0
			c := &module.Context{Env: lane.Envelope{Type: "channel.chat.message", Text: "Kappa", BroadcasterUserID: "42"}, BroadcasterID: 42,
				LocaleLookup: func(context.Context, uint64) (string, error) { calls++; return "fr", nil }}
			out := runEvent(t, EmotePlay(emotePlayDeps(store)), c)
			if !milestone {
				require.Zero(t, calls)
				require.Empty(t, out)
				return
			}
			require.Equal(t, 1, calls)
			require.Len(t, out, 1)
			require.Equal(t, "Série Kappa ×10 !", out[0].Text)
			require.Equal(t, "fr", c.Locale)
		})
	}
}
