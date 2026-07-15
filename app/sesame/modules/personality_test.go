package modules

import (
	"context"
	"fmt"
	"testing"

	"ItsBagelBot/app/sesame/engine"
	"ItsBagelBot/app/sesame/module"
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
	assert.Empty(t, m.Commands, "personality owns no commands")
	h := m.Events["channel.chat.message"]
	require.NotNil(t, h, "personality must handle channel.chat.message")
	return h
}

func personalityCtx(text string) *module.Context {
	return &module.Context{
		Env: lane.Envelope{
			Type:              "channel.chat.message",
			Text:              text,
			BroadcasterUserID: "2",
			ChatterUserName:   "Bob",
		},
		BroadcasterID: 2,
		Log:           zap.NewNop(),
	}
}

// fakePersonality scripts the PersonalityStore: fixed cursor/feed values, an
// optional sticky mood, and an optional error that fails every call.
type fakePersonality struct {
	cursor int64
	feed   int64
	mood   string
	err    error
}

func (f *fakePersonality) FactCursor(context.Context, uint64) (int64, error) {
	return f.cursor, f.err
}

func (f *fakePersonality) FeedCount(context.Context, uint64) (int64, error) {
	return f.feed, f.err
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
	require.NoError(t, personalityHandler(t, engine.Deps{})(context.Background(), personalityCtx("bagel fact please"), col.emit))
	require.Len(t, col.out, 1)
	assert.Equal(t, personalityFacts[0], col.out[0].Text)
}

func TestPersonalityFeedCounts(t *testing.T) {
	pinPersonalityRand(t)
	var col collector
	d := engine.Deps{Personality: &fakePersonality{feed: 3}}
	require.NoError(t, personalityHandler(t, d)(context.Background(), personalityCtx("feed the bagel"), col.emit))
	require.Len(t, col.out, 1)
	assert.Equal(t, fmt.Sprintf(personalityFeedCountPack[0], 3), col.out[0].Text)
}

func TestPersonalityFeedFallsBackOnStoreError(t *testing.T) {
	pinPersonalityRand(t)
	var col collector
	d := engine.Deps{Personality: &fakePersonality{err: assert.AnError}}
	require.NoError(t, personalityHandler(t, d)(context.Background(), personalityCtx("feed the bagel"), col.emit))
	require.Len(t, col.out, 1)
	assert.Equal(t, personalityFeedPlainPack[0], col.out[0].Text)
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
	var col collector
	require.NoError(t, personalityHandler(t, engine.Deps{})(context.Background(), personalityCtx("good bagel bot"), col.emit))
	require.Len(t, col.out, 1)
	assert.Equal(t, personalityGoodPack[0], col.out[0].Text, "praise must win over the generic mention fact")
}
