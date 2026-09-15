// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package scope

import "sort"

// TokenFamily is the documentation-facing description of one custom-command
// token family. Examples are complete lexer spellings (including braces), not
// just heads: a family such as counter or urlfetch is open-ended and therefore
// cannot be enumerated one name at a time.
//
// This is intentionally a small runtime catalogue, not a resolver. The scope
// implementations remain authoritative for behaviour and availability; this
// type gives tooling a stable, inspectable inventory of the names and shapes
// that the command chain can answer.
type TokenFamily struct {
	ID       string
	Examples []string
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
	message = append(message, "{1}", "{1:}")
	sort.Strings(message)

	pure := make([]string, 0, len(pureUtils)+2)
	for name := range pureUtils {
		pure = append(pure, "{"+name+":example}")
	}
	pure = append(pure, "{random}", "{choice:yes,no}")
	sort.Strings(pure)

	return []TokenFamily{
		{ID: "message", Examples: message},
		{ID: "pure", Examples: pure},
		{ID: "chatters", Examples: []string{"{" + ChattersToken + "}", "{" + RandomChatterToken + "}"}},
		{ID: "emotes", Examples: []string{"{" + SevenTVEmotesToken + "}", "{" + BTTVEmotesToken + "}", "{" + FFZEmotesToken + "}", "{" + RandomEmoteToken + "}"}},
		{ID: "channel", Examples: []string{"{" + UptimeToken + "}", "{" + TitleToken + ":other_channel}", "{" + GameToken + ":other_channel}", "{" + ViewersToken + "}"}},
		{ID: "viewer", Examples: []string{"{" + FollowageToken + "}", "{" + AccountAgeToken + ":viewer}", "{" + PointsToken + "}", "{" + PointsNameToken + "}", "{" + WatchTimeToken + "}"}},
		{ID: "modules", Examples: []string{"{" + QuoteToken + "}", "{" + QuoteToken + ":1}", "{" + TimeToken + "}", "{" + SongToken + "}", "{" + SongTitleToken + "}", "{" + SongArtistToken + "}"}},
		{ID: "uses", Examples: []string{"{uses}"}},
		{ID: "store", Examples: []string{"{counter:deaths}", "{counter:target:deaths}", "{count:deaths}", "{count:target:deaths}"}},
		{ID: "external", Examples: []string{"{urlfetch:weather}"}},
	}
}
