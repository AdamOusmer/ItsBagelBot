// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package scope

import "sort"

type TokenFamily struct {
	ID       string
	Examples []string
	Aliases  []string
}

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
