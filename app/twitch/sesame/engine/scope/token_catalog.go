// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package scope

import "sort"

// TokenFamily is the documentation-facing description of one custom-command
// token family. Examples are complete lexer spellings (including braces), not
// just heads: a family such as counter or urlfetch is open-ended and therefore
// cannot be enumerated one name at a time.
//
// Aliases are the family's pre-simplification spellings — also complete
// lexer spellings — kept separate from Examples rather than folded in: the
// public catalogue (and the guide it generates) teaches the canonical
// spelling only, but web/kit/lib/variables/parity.test.ts still needs a way
// to know a name the manifest lists as an alias is genuinely answered by the
// resolver, without either scraping Examples for something that looks like
// an alias or hand-keeping a second list on the TypeScript side that this
// package could silently drift from.
//
// This is intentionally a small runtime catalogue, not a resolver. The scope
// implementations remain authoritative for behaviour and availability; this
// type gives tooling a stable, inspectable inventory of the names and shapes
// that the command chain can answer.
type TokenFamily struct {
	ID       string
	Examples []string
	Aliases  []string
}

// CommandTokenFamilies returns the complete custom-command token inventory.
// The returned slices are independent, so callers may sort or annotate them.
// Dynamic payloads use representative names ("deaths", "weather", and so
// on); the resolver owns the naming rules for those payloads.
func CommandTokenFamilies() []TokenFamily {
	message := make([]string, 0, len(messageFields)+2)
	for name := range messageFields {
		message = append(message, "{"+name+"}")
	}
	message = append(message, "{1}", "{1:}", "{:2}", "{2:4}")
	sort.Strings(message)

	messageAliasExamples := make([]string, 0, len(messageAliases))
	for alias := range messageAliases {
		messageAliasExamples = append(messageAliasExamples, "{"+alias+"}")
	}
	sort.Strings(messageAliasExamples)

	pure := make([]string, 0, len(pureUtils)+2)
	for name := range pureUtils {
		pure = append(pure, "{"+name+":example}")
	}
	pure = append(pure, "{random}", "{choice:yes,no}")
	sort.Strings(pure)

	return []TokenFamily{
		{ID: "message", Examples: message, Aliases: messageAliasExamples},
		{ID: "pure", Examples: pure},
		{ID: "chatters", Examples: []string{"{" + ChattersToken + "}", "{" + RandomChatterToken + "}", "{" + RandomViewerToken + "}"}},
		{
			ID:       "emotes",
			Examples: []string{"{" + EmotesToken + ":7tv}", "{" + EmotesToken + ":bttv}", "{" + EmotesToken + ":ffz}", "{" + RandomEmoteToken + "}"},
			Aliases:  []string{"{" + SevenTVEmotesToken + "}", "{" + BTTVEmotesToken + "}", "{" + FFZEmotesToken + "}"},
		},
		{ID: "channel", Examples: []string{"{" + UptimeToken + "}", "{" + TitleToken + ":other_channel}", "{" + GameToken + ":other_channel}", "{" + ViewersToken + "}", "{" + FollowersToken + "}", "{" + SubsToken + "}"}},
		{
			ID:       "viewer",
			Examples: []string{"{" + FollowageToken + "}", "{" + AccountAgeToken + ":viewer}", "{" + PointsToken + "}", "{" + PointsNameToken + "}", "{" + WatchTimeToken + "}"},
			Aliases:  []string{"{" + PointsNameLegacyToken + "}"},
		},
		{ID: "modules", Examples: []string{"{" + QuoteToken + "}", "{" + QuoteToken + ":1}", "{" + TimeToken + "}", "{" + TimeToken + ":Paris}", "{" + SongToken + "}", "{" + SongTitleToken + "}", "{" + SongArtistToken + "}"}},
		{ID: "uses", Examples: []string{"{count}"}, Aliases: []string{"{uses}"}},
		{
			ID:       "store",
			Examples: []string{"{counter:deaths}", "{counter:target:deaths}"},
			Aliases:  []string{"{count:deaths}", "{count:target:deaths}"},
		},
		{ID: "external", Examples: []string{"{urlfetch:weather}"}},
	}
}

// TimerFamilies names the token families a timer's message can resolve
// (engine/timer_vars.go's timerChain), by CommandTokenFamilies' own family
// IDs. A timer fires off a schedule key with no chatter, no command args and
// no message line behind it, so message, viewer (the "who is asking" family)
// and store (a counter bump needs an identity to charge it to) are left out
// on purpose — the same three families timerChain does not mount. Everything
// else here is channel-scoped and safe to answer with nobody watching.
//
// This is the Go half of the timer surface handshake with
// web/kit/lib/variables/surfaces.ts's forSurface('timer'): both lists are
// checked against each other by parity.test.ts through
// testdata/token_catalog.golden.json's surfaces.timer key (regenerate with
// -scope.write-golden), so a family added to one side without the other
// fails there instead of drifting silently.
//
// Derived from CommandTokenFamilies() rather than its own hand-typed slice:
// timerAllowedFamilies below is the only place the set of six ids is spelled
// out, and filtering CommandTokenFamilies()'s own IDs through it (instead of
// listing "pure", "chatters", … a second time) is what makes a renamed or
// removed family fail LOUDLY here — a stale id in timerAllowedFamilies now
// simply matches nothing and quietly shrinks the result, which is why
// token_catalog_golden_test.go additionally asserts every name in
// timerAllowedFamilies is one CommandTokenFamilies() still has, and catches
// exactly that case.
func TimerFamilies() []string {
	allowed := timerAllowedFamilies()
	out := make([]string, 0, len(allowed))
	for _, family := range CommandTokenFamilies() {
		if allowed[family.ID] {
			out = append(out, family.ID)
		}
	}
	return out
}

// timerAllowedFamilies is the one place the timer surface's family set is
// spelled out: everything a tick has no chatter, no command args and no
// message line to answer from message, viewer (the "who is asking" family)
// and store (a counter bump needs an identity to charge it to) — is left
// out, matching engine/timer_vars.go's timerChain exactly.
func timerAllowedFamilies() map[string]bool {
	return map[string]bool{
		"pure":     true,
		"chatters": true,
		"emotes":   true,
		"channel":  true,
		"modules":  true,
		"external": true,
	}
}
