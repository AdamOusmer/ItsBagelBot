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
	"ItsBagelBot/internal/domain/i18n"
	"ItsBagelBot/internal/domain/outgress"
	"ItsBagelBot/pkg/tmpl"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func patchPersonalityRand(t *testing.T, pick func(int) int, golden func() bool) {
	t.Helper()
	oldPick, oldGolden := pickIndex, goldenRoll
	pickIndex, goldenRoll = pick, golden
	t.Cleanup(func() { pickIndex, goldenRoll = oldPick, oldGolden })
}

func pinPersonalityRand(t *testing.T) {
	t.Helper()
	patchPersonalityRand(t, func(int) int { return 0 }, func() bool { return false })
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

type fakePersonality struct {
	cursor  int64
	feed    engine.FeedCounts
	board   engine.FeedBoard
	mood    string
	err     error
	fedBy   uint64
	fedName string
	writes  []string
}

func (f *fakePersonality) FactCursor(context.Context, uint64) (int64, error) {
	f.writes = append(f.writes, "cursor")
	return f.cursor, f.err
}

func (f *fakePersonality) Feed(_ context.Context, broadcasterID uint64, name, _ string) (engine.FeedCounts, error) {
	f.writes = append(f.writes, "feed")
	f.fedBy, f.fedName = broadcasterID, name
	return f.feed, f.err
}

func (f *fakePersonality) FeedBoard(context.Context, uint64, int) (engine.FeedBoard, error) {
	return f.board, f.err
}

func (f *fakePersonality) Mood(_ context.Context, _ uint64, candidate string) (string, error) {
	f.writes = append(f.writes, "mood")
	if f.mood == "" {
		return candidate, f.err
	}
	return f.mood, f.err
}

func expandFor(line string) string {
	return module.ExpandString(line, func(tok tmpl.Token) (string, bool) {
		if tok.Key() == "user" {
			return "Bob", true
		}
		return tmpl.Dynamic(tok)
	})
}

type personalityCase struct {
	name         string
	text         string
	store        *fakePersonality
	locale       string
	lookupLocale string
	golden       bool
	emojiPick    int
	cohort       bool
	want         string
}

var personalityCases = []personalityCase{
	{name: "good bagel gets praise", text: "Good bagel!", want: personalityGoodPack[0]},
	{name: "a user token expands", text: "hug the bagel", want: "hugging Bob. careful. I crumble under pressure. literally."},
	{name: "the bagelbot spelling is praised too", text: "good bot", want: personalityGoodPack[0]},
	{name: "bad bot is scolded", text: "bad bot", want: personalityBadPack[0]},
	{name: "a directed good night", text: "Good night @ItsBagelBot", want: personalityGnPack[0]},
	{name: "a short good night", text: "gn, @ItsBagelBot!!", want: personalityGnPack[0]},
	{name: "a french good night", text: "bonne nuit itsbagelbot", want: personalityGnPack[0]},
	{name: "thanks are answered", text: "thanks itsbagelbot", want: personalityThanksPack[0]},
	{name: "a praise sentence wins over the mention", text: "you are a good bagelbot", want: personalityGoodPack[0]},
	{name: "a scolding wins over the mention", text: "bad @ItsBagelBot. very bad bot", want: personalityBadPack[0]},
	{name: "the first matching phrase wins", text: "good bagel, gn bagel", want: personalityGnPack[0]},
	{name: "an @mention walks the fact cursor modulo the list", text: "@ItsBagelBot tell me things", store: &fakePersonality{cursor: int64(len(personalityFacts)) + 5}, want: personalityFacts[4]},
	{name: "a fact falls back without a store", text: "@ItsBagelBot tell me something", want: personalityFacts[0]},
	{name: "a bare mention gets a fact", text: "@ItsBagelBot", store: &fakePersonality{cursor: 1}, want: personalityFacts[0]},
	{name: "a mention inside a sentence gets a fact", text: "hey @itsbagelbot, listen", store: &fakePersonality{cursor: 1}, want: personalityFacts[0]},
	{name: "a mention with punctuation gets a fact", text: "@ItsBagelBot!", store: &fakePersonality{cursor: 1}, want: personalityFacts[0]},
	{name: "feeding reports today and lifetime", text: "feed the bagel", store: &fakePersonality{feed: engine.FeedCounts{Today: 3, Total: 48213}},
		want: fmt.Sprintf(personalityFeedCountPack[0], 3, 48213)},
	{name: "feeding the bagelbot works", text: "feed the bagelbot", store: &fakePersonality{feed: engine.FeedCounts{Today: 2, Total: 7}}, want: fmt.Sprintf(personalityFeedCountPack[0], 2, 7)},
	{name: "feeding itsbagelbot works", text: "feed itsbagelbot", store: &fakePersonality{feed: engine.FeedCounts{Today: 2, Total: 7}}, want: fmt.Sprintf(personalityFeedCountPack[0], 2, 7)},
	{name: "feeding the bagel bot works", text: "feed the bagel bot", store: &fakePersonality{feed: engine.FeedCounts{Today: 2, Total: 7}}, want: fmt.Sprintf(personalityFeedCountPack[0], 2, 7)},
	{name: "feeding an @mention works", text: "feed @ItsBagelBot", store: &fakePersonality{feed: engine.FeedCounts{Today: 2, Total: 7}}, want: fmt.Sprintf(personalityFeedCountPack[0], 2, 7)},
	{name: "a store error silences the feed line", text: "feed the bagel", store: &fakePersonality{err: assert.AnError}},
	{name: "no store means no feed line", text: "feed the bagel"},
	{name: "the stored mood sticks", text: "bagel mood?", store: &fakePersonality{mood: personalityMoodPack[2]}, want: "current mood: " + personalityMoodPack[2]},
	{name: "a toast rolls a level", text: "toast the bagel", want: fmt.Sprintf(personalityToastLines[0], 0)},
	{name: "the golden bagel overrides the reply", text: "pet the bagel", golden: true, want: "GOLDEN BAGEL"},
	{name: "a 1-in-N emoji reaction fires on a zero roll", text: "nice stream 🥯", want: personalityEmojiPack[0]},
	{name: "a 1-in-N emoji reaction respects its gate", text: "nice stream 🥯", emojiPick: 1},
	{name: "commands are skipped", text: "!bagel mood"},
	{name: "blank messages are skipped", text: "   "},
	{name: "plain chat is skipped", text: "hello everyone"},
	{name: "folded duplicate cohorts are skipped", text: "good bagel", cohort: true},
	{name: "bad bagels are not a scolding", text: "bad bagels are rare"},
	{name: "a glued word is not a trigger", text: "goodbagel"},
	{name: "a longer word is not a trigger", text: "the bagelbots rise"},
	{name: "a bare bot name gets no fact", text: "yo bagelbot"},
	{name: "a spaced bot name gets no fact", text: "its bagel bot is here"},
	{name: "a bare itsbagelbot gets no fact", text: "itsbagelbot what is up"},
	{name: "asking for a bagel fact gets none", text: "bagel fact"},
	{name: "asking for bagel facts gets none", text: "bagel facts please"},
	{name: "loving a bagel gets none", text: "I love a warm bagel"},
	{name: "a lookalike handle gets none", text: "@itsbagelbotfake is a copy"},
	{name: "french praise", text: "bon bagel", lookupLocale: "fr", want: "Validation reçue. crise existentielle reportée."},
	{name: "french hug", text: "câlin bagel", lookupLocale: "fr", want: "Câlin à Bob. doucement, je m'émiette sous la pression. littéralement."},
	{name: "french feeding", text: "nourris le bagel", lookupLocale: "fr", store: &fakePersonality{feed: engine.FeedCounts{Today: 3, Total: 7}},
		want: "Miam. Ça fait 3 repas aujourd'hui, 7 au total. Aucun regret. Quelques regrets."},
	{name: "french mood", text: "humeur du bagel", lookupLocale: "fr", store: &fakePersonality{mood: personalityMoodPack[2]},
		want: "humeur actuelle : nature. minimaliste. ne me teste pas."},
	{name: "french fact", text: "@ItsBagelBot", lookupLocale: "fr", store: &fakePersonality{cursor: 1},
		want: "La première mention écrite du bagel date de 1610 à Cracovie, en Pologne. Mes papiers sont plus vieux que bien des pays."},
	{name: "french toast", text: "grille le bagel", lookupLocale: "fr", want: "niveau de grillage 0/10 : tu appelles ça griller ? J'ai senti un courant d'air."},
	{name: "an unknown locale falls back to english", text: "bon bagel", locale: "xx", want: personalityGoodPack[0]},
	{name: "the french golden bagel expands the user", text: "bon bagel", locale: "fr", golden: true, want: "BAGEL DORÉ"},
}

func TestPersonalityChat(t *testing.T) {
	cases := personalityCases
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pick := func(int) int { return tc.emojiPick }
			patchPersonalityRand(t, pick, func() bool { return tc.golden })
			d := engine.Deps{}
			if tc.store != nil {
				store := *tc.store
				d.Personality = &store
			}
			c := personalityCtx(tc.text)
			c.Locale = tc.locale
			lookups := 0
			if tc.lookupLocale != "" {
				c.LocaleLookup = func(context.Context, uint64) (string, error) { lookups++; return tc.lookupLocale, nil }
			}
			if tc.cohort {
				c.Env.Senders = []lane.Sender{{}}
			}
			out := runEvent(t, Personality(d), c)
			if tc.want == "" {
				assert.Empty(t, out)
				assert.Zero(t, lookups, "the locale loads only on a match")
				return
			}
			require.Len(t, out, 1)
			assert.Equal(t, outgress.TypeChat, out[0].Type)
			assert.Equal(t, "2", out[0].BroadcasterID)
			if tc.golden {
				assert.Contains(t, out[0].Text, tc.want)
				assert.Contains(t, out[0].Text, "Bob")
				assert.NotContains(t, out[0].Text, "{personality:user}")
				return
			}
			assert.Equal(t, expandFor(tc.want), out[0].Text)
			if tc.lookupLocale != "" {
				assert.Equal(t, 1, lookups, "the locale loads once, on a match")
			}
		})
	}
}

func TestPersonalityFeedNamesTheChannel(t *testing.T) {
	pinPersonalityRand(t)
	store := &fakePersonality{feed: engine.FeedCounts{Today: 3, Total: 7}}
	runEvent(t, Personality(engine.Deps{Personality: store}), personalityCtx("feed the bagel"))
	assert.Equal(t, uint64(2), store.fedBy, "the feeding must name the channel that fed, for its leaderboard row")
	assert.Equal(t, "Chan", store.fedName, "the display name rides along so the board can name the channel")
}

func TestPersonalityTrialLeavesStateUntouched(t *testing.T) {
	pinPersonalityRand(t)
	for _, tc := range []struct {
		name string
		text string
		want []string
	}{
		{"cooldown", "good bagel", []string{personalityGoodPack[0]}},
		{"cursor", "@ItsBagelBot", []string{personalityFacts[0]}},
		{"feed", "feed the bagel", nil},
		{"mood", "bagel mood?", []string{"current mood: " + personalityMoodPack[0]}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &fakePersonality{cursor: 6, mood: personalityMoodPack[2]}
			cooldown := &fakeCooldown{allow: []bool{false}}
			c := personalityCtx(tc.text)
			c.Env.Origin = "trial"
			out := runEvent(t, Personality(engine.Deps{Personality: store, Cooldown: cooldown}), c)
			assert.Equal(t, tc.want, texts(out), "trial evaluation uses observation replies")
			assert.Empty(t, cooldown.keys, "trial evaluation must not claim a cooldown")
			assert.Equal(t, []bool{false}, cooldown.allow, "the cooldown remains available to normal processing")
			assert.Empty(t, store.writes, "trial evaluation must not call cursor, feed, or mood writes")
		})
	}
}

func TestPersonalityCooldownGates(t *testing.T) {
	pinPersonalityRand(t)
	cd := &fakeCooldown{allow: []bool{true, false}}
	m := Personality(engine.Deps{Cooldown: cd})
	require.Len(t, runEvent(t, m, personalityCtx("good bagel")), 1)
	assert.Empty(t, runEvent(t, m, personalityCtx("good bagel")), "second hit inside the window must stay silent")
	require.Len(t, cd.keys, 2)
	assert.Equal(t, "personality:cd:good:2", cd.keys[0])
}

func TestFeedCommands(t *testing.T) {
	board := engine.FeedBoard{
		Entries: []engine.FeedBoardEntry{{BroadcasterID: 9, Name: "Crumb", Count: 400}, {BroadcasterID: 8, Count: 120}},
		Ranked:  57, Channel: 12, Rank: 4,
	}
	cases := []struct {
		name     string
		text     string
		store    *fakePersonality
		exact    string
		contains []string
	}{
		{name: "bagels reports the channel standing", text: "!bagels", store: &fakePersonality{board: engine.FeedBoard{Ranked: 57, Channel: 12, Rank: 4}},
			exact: "Chan has fed the bagel 12 times: #4 of 57 channels."},
		{name: "bagels nudges an unranked channel", text: "!bagels", store: &fakePersonality{board: engine.FeedBoard{Ranked: 57}}, contains: []string{"never fed the bagel"}},
		{name: "bagelboard ranks channels, a nameless row under its id", text: "!bagelboard", store: &fakePersonality{board: board},
			exact: "Bagel leaderboard: 1. Crumb (400), 2. channel 8 (120). This channel: 12 feedings, #4 of 57."},
		{name: "bagelboard on an empty board", text: "!bagelboard", store: &fakePersonality{}, contains: []string{"Nobody has fed the bagel yet"}},
		{name: "bagels reports unavailable on a store error", text: "!bagels", store: &fakePersonality{err: assert.AnError}, contains: []string{"cannot count right now"}},
		{name: "bagelboard reports unavailable on a store error", text: "!bagelboard", store: &fakePersonality{err: assert.AnError}, contains: []string{"cannot count right now"}},
		{name: "the fed alias answers like bagels", text: "!fed", store: &fakePersonality{board: engine.FeedBoard{Ranked: 57, Channel: 12, Rank: 4}},
			exact: "Chan has fed the bagel 12 times: #4 of 57 channels."},
		{name: "bagels is silent without a store", text: "!bagels"},
		{name: "bagelboard is silent without a store", text: "!bagelboard"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := engine.Deps{}
			if tc.store != nil {
				d.Personality = tc.store
			}
			out := runChat(t, Personality(d), personalityCtx(""), tc.text)
			if tc.exact == "" && tc.contains == nil {
				assert.Empty(t, out)
				return
			}
			require.Len(t, out, 1)
			assertText(t, out[0].Text, textWant{tc.exact, tc.contains, nil})
			if tc.store != nil {
				assert.Zero(t, tc.store.fedBy, "reading the standing must not record a feeding")
			}
		})
	}
}

func TestPersonalityRepliesWithUnpinnedRandomness(t *testing.T) {
	out := runEvent(t, Personality(engine.Deps{}), personalityCtx("good bagel"))
	require.Len(t, out, 1, "a golden override replaces the line, it never adds a second one")
}

func TestPersonalityRespondsToEveryReactionPhrase(t *testing.T) {
	pinPersonalityRand(t)
	for _, r := range personalityReactions {
		for _, p := range r.phrases {
			t.Run(r.name+"/"+p, func(t *testing.T) {
				d := engine.Deps{Personality: &fakePersonality{feed: engine.FeedCounts{Today: 1, Total: 1}}}
				assert.Len(t, runEvent(t, Personality(d), personalityCtx(p)), 1, "a table phrase must never fall outside the gate")
			})
		}
	}
}

func TestPersonalityLocalesCoverEveryLine(t *testing.T) {
	missingFrench := i18n.Missing("fr")
	packs := map[string][]string{
		"good": personalityGoodPack, "bad": personalityBadPack,
		"give": personalityGiveBagel, "thanks": personalityThanksPack,
		"affection": personalityAffectionPack, "feed": personalityFeedCountPack,
		"boop": personalityBoopPack, "gn": personalityGnPack,
		"mood": personalityMoodPack, "emoji": personalityEmojiPack,
		"toast": personalityToastLines, "fact": personalityFacts,
	}
	for name, pack := range packs {
		for index, english := range pack {
			key := fmt.Sprintf("personality.%s.%d", name, index)
			assert.Equal(t, english, i18n.T("en", key), key)
			assert.NotContains(t, missingFrench, key)
			assert.NotEqual(t, key, i18n.T("fr", key), key)
		}
	}
	assert.NotEqual(t, "personality.golden", i18n.T("en", "personality.golden"))
	assert.NotEqual(t, "personality.golden", i18n.T("fr", "personality.golden"))
	assert.Equal(t, "current mood: ", i18n.T("en", "personality.mood.prefix"))
	assert.Equal(t, "humeur actuelle : ", i18n.T("fr", "personality.mood.prefix"))
}
