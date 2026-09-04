// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"context"
	"fmt"
	"testing"

	"ItsBagelBot/app/twitch/sesame/engine"
	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/internal/domain/event/lane"
	"ItsBagelBot/internal/domain/outgress"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// pinPersonalityRand makes the module deterministic for a test: every pick
// lands on index 0 (first pack line, toast level 0, chance gates pass) and the
// golden roll never fires. Restored on cleanup.
func pinPersonalityRand(t *testing.T) {
	t.Helper()
	oldPick, oldGolden := pickIndex, goldenRoll
	pickIndex = func(int) int { return 0 }
	goldenRoll = func() bool { return false }
	t.Cleanup(func() { pickIndex, goldenRoll = oldPick, oldGolden })
}

func personalityHandler(t *testing.T, d engine.Deps) module.EventHandler {
	t.Helper()
	m := Personality(d)
	assert.Equal(t, "personality", m.Name)
	assert.Equal(t, module.KindCore, m.Kind, "personality must be a core module: always on, not removable")
	assert.Len(t, m.Commands, 2, "personality owns the two feed-leaderboard commands")
	h := m.Events["channel.chat.message"]
	require.NotNil(t, h, "personality must handle channel.chat.message")
	return h
}

// personalityCommand returns one of the module's baked commands by trigger.
func personalityCommand(t *testing.T, d engine.Deps, name string) module.RunFunc {
	t.Helper()
	for _, cmd := range Personality(d).Commands {
		if cmd.Name == name {
			return cmd.Run
		}
	}
	t.Fatalf("personality has no %q command", name)
	return nil
}

func personalityCtx(text string) *module.Context {
	return &module.Context{
		Env: lane.Envelope{
			Type:                "channel.chat.message",
			Text:                text,
			BroadcasterUserID:   "2",
			BroadcasterUserName: "Chan",
			ChatterUserName:     "Bob",
		},
		BroadcasterID: 2,
		Log:           zap.NewNop(),
	}
}

// fakePersonality scripts the PersonalityStore: fixed cursor/feed values, an
// optional sticky mood, an optional leaderboard, and an optional error that
// fails every call. fedBy records the channel the last feeding named.
type fakePersonality struct {
	cursor int64
	feed   engine.FeedCounts
	board  engine.FeedBoard
	mood   string
	err    error

	fedBy   uint64
	fedName string
}

func (f *fakePersonality) FactCursor(context.Context, uint64) (int64, error) {
	return f.cursor, f.err
}

func (f *fakePersonality) Feed(_ context.Context, broadcasterID uint64, name string) (engine.FeedCounts, error) {
	f.fedBy, f.fedName = broadcasterID, name
	return f.feed, f.err
}

func (f *fakePersonality) FeedBoard(_ context.Context, _ uint64, _ int) (engine.FeedBoard, error) {
	return f.board, f.err
}

func (f *fakePersonality) Mood(_ context.Context, _ uint64, candidate string) (string, error) {
	if f.mood == "" {
		return candidate, f.err
	}
	return f.mood, f.err
}

func TestPersonalitySkipsCommandsCohortsAndPlainChat(t *testing.T) {
	pinPersonalityRand(t)
	h := personalityHandler(t, engine.Deps{})
	for name, c := range map[string]*module.Context{
		"command":    personalityCtx("!bagel mood"),
		"empty":      personalityCtx("   "),
		"no trigger": personalityCtx("hello everyone"),
	} {
		var col collector
		require.NoError(t, h(context.Background(), c, col.emit), name)
		assert.Empty(t, col.out, name)
	}

	var col collector
	cohort := personalityCtx("good bagel")
	cohort.Env.Senders = []lane.Sender{{}}
	require.NoError(t, h(context.Background(), cohort, col.emit))
	assert.Empty(t, col.out, "folded duplicate cohorts must not trigger reactions")
}

func TestPersonalityGoodBagel(t *testing.T) {
	pinPersonalityRand(t)
	var col collector
	require.NoError(t, personalityHandler(t, engine.Deps{})(context.Background(), personalityCtx("Good bagel!"), col.emit))
	require.Len(t, col.out, 1)
	assert.Equal(t, outgress.TypeChat, col.out[0].Type)
	assert.Equal(t, "2", col.out[0].BroadcasterID)
	assert.Equal(t, personalityGoodPack[0], col.out[0].Text)
}

func TestPersonalityWordBoundary(t *testing.T) {
	pinPersonalityRand(t)
	h := personalityHandler(t, engine.Deps{})
	for _, text := range []string{"bad bagels are rare", "goodbagel", "the bagelbots rise"} {
		var col collector
		require.NoError(t, h(context.Background(), personalityCtx(text), col.emit))
		assert.Empty(t, col.out, text)
	}
}

func TestPersonalityExpandsUser(t *testing.T) {
	pinPersonalityRand(t)
	var col collector
	require.NoError(t, personalityHandler(t, engine.Deps{})(context.Background(), personalityCtx("hug the bagel"), col.emit))
	require.Len(t, col.out, 1)
	assert.Equal(t, "hugging Bob. careful. I crumble under pressure. literally.", col.out[0].Text)
}

func TestPersonalityMentionWalksFactCursor(t *testing.T) {
	pinPersonalityRand(t)
	store := &fakePersonality{cursor: int64(len(personalityFacts)) + 5}
	var col collector
	require.NoError(t, personalityHandler(t, engine.Deps{Personality: store})(context.Background(), personalityCtx("@ItsBagelBot tell me things"), col.emit))
	require.Len(t, col.out, 1)
	assert.Equal(t, personalityFacts[4], col.out[0].Text, "cursor must wrap modulo the fact list")
}

func TestPersonalityFactFallsBackWithoutStore(t *testing.T) {
	pinPersonalityRand(t)
	var col collector
	require.NoError(t, personalityHandler(t, engine.Deps{})(context.Background(), personalityCtx("@ItsBagelBot tell me something"), col.emit))
	require.Len(t, col.out, 1)
	assert.Equal(t, personalityFacts[0], col.out[0].Text)
}

func TestPersonalityFeedReportsTodayAndLifetime(t *testing.T) {
	pinPersonalityRand(t)
	var col collector
	store := &fakePersonality{feed: engine.FeedCounts{Today: 3, Total: 48213}}
	require.NoError(t, personalityHandler(t, engine.Deps{Personality: store})(context.Background(), personalityCtx("feed the bagel"), col.emit))
	require.Len(t, col.out, 1)
	assert.Equal(t, fmt.Sprintf(personalityFeedCountPack[0], 3, 48213), col.out[0].Text)
	assert.Equal(t, uint64(2), store.fedBy, "the feeding must name the channel that fed, for its leaderboard row")
	assert.Equal(t, "Chan", store.fedName, "the display name rides along so the board can name the channel")
}

func TestPersonalityFeedSilentWithoutCounts(t *testing.T) {
	pinPersonalityRand(t)
	h := personalityHandler(t, engine.Deps{Personality: &fakePersonality{err: assert.AnError}})

	var onErr collector
	require.NoError(t, h(context.Background(), personalityCtx("feed the bagel"), onErr.emit))
	assert.Empty(t, onErr.out, "a store error must silence the feed line, not degrade it")

	var noStore collector
	require.NoError(t, personalityHandler(t, engine.Deps{})(context.Background(), personalityCtx("feed the bagel"), noStore.emit))
	assert.Empty(t, noStore.out, "no store, no feed line")
}

func TestPersonalityMoodSticksToStoredValue(t *testing.T) {
	pinPersonalityRand(t)
	var col collector
	d := engine.Deps{Personality: &fakePersonality{mood: personalityMoodPack[2]}}
	require.NoError(t, personalityHandler(t, d)(context.Background(), personalityCtx("bagel mood?"), col.emit))
	require.Len(t, col.out, 1)
	assert.Equal(t, "current mood: "+personalityMoodPack[2], col.out[0].Text)
}

func TestPersonalityToastRollsALevel(t *testing.T) {
	pinPersonalityRand(t)
	var col collector
	require.NoError(t, personalityHandler(t, engine.Deps{})(context.Background(), personalityCtx("toast the bagel"), col.emit))
	require.Len(t, col.out, 1)
	assert.Equal(t, fmt.Sprintf(personalityToastLines[0], 0), col.out[0].Text)
}

func TestPersonalityCooldownGates(t *testing.T) {
	pinPersonalityRand(t)
	cd := &fakeCooldown{allow: []bool{true, false}}
	h := personalityHandler(t, engine.Deps{Cooldown: cd})

	var first collector
	require.NoError(t, h(context.Background(), personalityCtx("good bagel"), first.emit))
	require.Len(t, first.out, 1)

	var second collector
	require.NoError(t, h(context.Background(), personalityCtx("good bagel"), second.emit))
	assert.Empty(t, second.out, "second hit inside the window must stay silent")
	require.Len(t, cd.keys, 2)
	assert.Equal(t, "personality:cd:good:2", cd.keys[0])
}

func TestPersonalityGoldenOverride(t *testing.T) {
	pinPersonalityRand(t)
	goldenRoll = func() bool { return true }
	var col collector
	require.NoError(t, personalityHandler(t, engine.Deps{})(context.Background(), personalityCtx("pet the bagel"), col.emit))
	require.Len(t, col.out, 1)
	assert.Contains(t, col.out[0].Text, "GOLDEN BAGEL")
	assert.Contains(t, col.out[0].Text, "Bob")
}

func TestPersonalityEmojiChanceGate(t *testing.T) {
	pinPersonalityRand(t)
	pickIndex = func(int) int { return 1 } // chance gate misses (non-zero roll)
	h := personalityHandler(t, engine.Deps{})

	var muted collector
	require.NoError(t, h(context.Background(), personalityCtx("nice stream 🥯"), muted.emit))
	assert.Empty(t, muted.out, "emoji reaction must respect its 1-in-N gate")

	pickIndex = func(int) int { return 0 }
	var col collector
	require.NoError(t, h(context.Background(), personalityCtx("nice stream 🥯"), col.emit))
	require.Len(t, col.out, 1)
	assert.Equal(t, personalityEmojiPack[0], col.out[0].Text)
}

func TestPersonalitySpecificReactionBeatsMentionFact(t *testing.T) {
	pinPersonalityRand(t)
	h := personalityHandler(t, engine.Deps{})
	for text, want := range map[string]string{
		"good bagel bot":                 personalityGoodPack[0],
		"Good night @ItsBagelBot":        personalityGnPack[0],
		"gn, @ItsBagelBot!!":             personalityGnPack[0],
		"bonne nuit itsbagelbot":         personalityGnPack[0],
		"thanks itsbagelbot":             personalityThanksPack[0],
		"you are a good bagelbot":        personalityGoodPack[0],
		"bad @ItsBagelBot. very bad bot": personalityBadPack[0],
	} {
		var col collector
		require.NoError(t, h(context.Background(), personalityCtx(text), col.emit))
		require.Len(t, col.out, 1, text)
		assert.Equal(t, expandFor(want), col.out[0].Text, text)
	}
}

// expandFor renders a pack line the way packReply would for the test chatter.
func expandFor(line string) string {
	return module.ExpandString(line, func(key string) (string, bool) {
		if key == "user" {
			return "Bob", true
		}
		return module.ParseDynamic(key)
	})
}

func TestPersonalityNameVariantsReachDirectedReactions(t *testing.T) {
	pinPersonalityRand(t)
	d := engine.Deps{Personality: &fakePersonality{feed: engine.FeedCounts{Today: 2, Total: 7}}}
	h := personalityHandler(t, d)
	for _, text := range []string{"feed the bagelbot", "feed itsbagelbot", "feed the bagel bot", "feed @ItsBagelBot"} {
		var col collector
		require.NoError(t, h(context.Background(), personalityCtx(text), col.emit))
		require.Len(t, col.out, 1, text)
		assert.Equal(t, fmt.Sprintf(personalityFeedCountPack[0], 2, 7), col.out[0].Text, text)
	}
}

func TestPersonalityFactOnlyOnAtMention(t *testing.T) {
	pinPersonalityRand(t)
	store := &fakePersonality{cursor: 1}
	h := personalityHandler(t, engine.Deps{Personality: store})
	for _, text := range []string{"@ItsBagelBot", "hey @itsbagelbot, listen", "@ItsBagelBot!"} {
		var col collector
		require.NoError(t, h(context.Background(), personalityCtx(text), col.emit))
		require.Len(t, col.out, 1, text)
		assert.Equal(t, personalityFacts[0], col.out[0].Text, text)
	}

	// Everything short of an @-mention is silent: the written-out handles, the
	// old "bagel fact" phrases, and the food itself.
	for _, text := range []string{
		"yo bagelbot",
		"its bagel bot is here",
		"itsbagelbot what is up",
		"bagel fact",
		"bagel facts please",
		"I love a warm bagel",
		"@itsbagelbotfake is a copy",
	} {
		var col collector
		require.NoError(t, h(context.Background(), personalityCtx(text), col.emit))
		assert.Empty(t, col.out, text)
	}
}

func TestPersonalityNormalizeChat(t *testing.T) {
	assert.Equal(t, "good night itsbagelbot", normalizeChat("good night, @itsbagelbot!!"))
	assert.Equal(t, "gn bagel", normalizeChat("gn   bagel 🥯"))
	assert.Equal(t, "", normalizeChat("!?@"))
}

// !bagels answers with this channel's own count and rank; it must never feed
// the bagel on the way.
func TestFeedRankCommandReportsChannelStanding(t *testing.T) {
	store := &fakePersonality{board: engine.FeedBoard{Ranked: 57, Channel: 12, Rank: 4}}
	var col collector
	run := personalityCommand(t, engine.Deps{Personality: store}, "bagels")
	require.NoError(t, run(context.Background(), personalityCtx("!bagels"), "", col.emit))
	require.Len(t, col.out, 1)
	assert.Equal(t, "Chan has fed the bagel 12 times: #4 of 57 channels.", col.out[0].Text)
	assert.Zero(t, store.fedBy, "reading the standing must not record a feeding")
}

func TestFeedRankCommandNudgesUnrankedChannel(t *testing.T) {
	var col collector
	run := personalityCommand(t, engine.Deps{Personality: &fakePersonality{board: engine.FeedBoard{Ranked: 57}}}, "bagels")
	require.NoError(t, run(context.Background(), personalityCtx("!bagels"), "", col.emit))
	require.Len(t, col.out, 1)
	assert.Contains(t, col.out[0].Text, "never fed the bagel")
}

func TestFeedBoardCommandRanksChannels(t *testing.T) {
	board := engine.FeedBoard{
		Entries: []engine.FeedBoardEntry{
			{BroadcasterID: 9, Name: "Crumb", Count: 400},
			{BroadcasterID: 8, Count: 120},
		},
		Ranked: 57, Channel: 12, Rank: 4,
	}
	var col collector
	run := personalityCommand(t, engine.Deps{Personality: &fakePersonality{board: board}}, "bagelboard")
	require.NoError(t, run(context.Background(), personalityCtx("!bagelboard"), "", col.emit))
	require.Len(t, col.out, 1)
	assert.Equal(t,
		"Bagel leaderboard: 1. Crumb (400), 2. channel 8 (120). This channel: 12 feedings, #4 of 57.",
		col.out[0].Text, "a nameless row still ranks, under its id")
}

func TestFeedBoardCommandOnEmptyBoard(t *testing.T) {
	var col collector
	run := personalityCommand(t, engine.Deps{Personality: &fakePersonality{}}, "bagelboard")
	require.NoError(t, run(context.Background(), personalityCtx("!bagelboard"), "", col.emit))
	require.Len(t, col.out, 1)
	assert.Contains(t, col.out[0].Text, "Nobody has fed the bagel yet")
}

// A failing read says so rather than going silent: unlike the chat reaction,
// a command was asked a direct question and owes an answer.
func TestFeedCommandsReportUnavailableOnError(t *testing.T) {
	for _, name := range []string{"bagels", "bagelboard"} {
		var col collector
		run := personalityCommand(t, engine.Deps{Personality: &fakePersonality{err: assert.AnError}}, name)
		require.NoError(t, run(context.Background(), personalityCtx("!"+name), "", col.emit), name)
		require.Len(t, col.out, 1, name)
		assert.Contains(t, col.out[0].Text, "cannot count right now", name)
	}
}

// Without a store the commands stay quiet: nothing to read, nothing to say.
func TestFeedCommandsSilentWithoutStore(t *testing.T) {
	for _, name := range []string{"bagels", "bagelboard"} {
		var col collector
		run := personalityCommand(t, engine.Deps{}, name)
		require.NoError(t, run(context.Background(), personalityCtx("!"+name), "", col.emit), name)
		assert.Empty(t, col.out, name)
	}
}

// --- the derived gate ---

// TestPersonalityGateCoversEveryTablePhrase pins the derivation rather than
// the three terms it currently produces: the gate exists so that adding a
// reaction row cannot leave that row unreachable, and only a walk of the table
// itself can catch a row the greedy cover missed. "good bot" and "bad bot" are
// the rows that make this more than a formality — they name no bagel, so a
// hand-written bagel-only gate would silently kill them.
func TestPersonalityGateCoversEveryTablePhrase(t *testing.T) {
	for _, r := range personalityReactions {
		for _, p := range r.phrases {
			assert.True(t, personalityGate.screens(p), "%s row: %q outside the gate", r.name, p)
		}
	}

	pinPersonalityRand(t)
	h := personalityHandler(t, engine.Deps{})
	for text, want := range map[string]string{
		"good bot": personalityGoodPack[0],
		"bad bot":  personalityBadPack[0],
	} {
		var col collector
		require.NoError(t, h(context.Background(), personalityCtx(text), col.emit))
		require.Len(t, col.out, 1, text)
		assert.Equal(t, expandFor(want), col.out[0].Text, text)
	}

	// The other half of the screen: a line holding no gate term never reaches
	// the table, which is the whole point of running it first.
	assert.False(t, personalityGate.screens("nice weather for a croissant"))
	_, ok := matchReaction("nice weather for a croissant")
	assert.False(t, ok)
}

// TestPersonalityFirstMatchWinsThroughGate keeps the gate a screen and not a
// reorder. The line is the exact counterexample recorded on
// personalityReactions: an automaton reporting the earliest-ENDING pattern
// would answer praise here, where the table answers goodnight because gn sits
// above good.
func TestPersonalityFirstMatchWinsThroughGate(t *testing.T) {
	r, ok := matchReaction("good bagel, gn bagel")
	require.True(t, ok)
	assert.Equal(t, "gn", r.name)

	r, ok = matchReaction("good bagel")
	require.True(t, ok)
	assert.Equal(t, "good", r.name)
}
