// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package scope

import (
	"ItsBagelBot/internal/domain/modulevars"
	"ItsBagelBot/pkg/tmpl"
	"sort"
)

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

	families := []TokenFamily{
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
	// Preserve each legacy token head's existing family when publishing namespace forms.
	owners := make(map[string]int)
	for i, family := range families {
		for _, example := range append(append([]string{}, family.Examples...), family.Aliases...) {
			owners[tmpl.Lex(example)[0].Name] = i
		}
	}
	for _, example := range moduleNamespaceExamples() {
		i, found := owners[tmpl.Lex(example)[0].Name]
		if !found {
			i = 6
		}
		families[i].Examples = append(families[i].Examples, example)
	}
	return families

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

func moduleNamespaceExamples() []string {
	var examples []string
	for _, spec := range modulevars.Catalog() {
		seen := make(map[string]bool)
		for _, group := range spec.Groups {
			for _, field := range group.Fields {
				if !seen[field] {
					examples = append(examples, "{"+spec.ID+":"+field+"}")
					seen[field] = true
				}
				examples = append(examples, "{"+spec.ID+":"+group.Name+":"+field+"}")
			}
		}
	}
	return examples
}
