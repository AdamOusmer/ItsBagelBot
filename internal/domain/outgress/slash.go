// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package outgress

import "strings"

type SlashCommand struct {
	Type  string
	Color string
	To    string
	Text  string
}

func CutSlash(text string) (SlashCommand, bool) {
	if color, rest, ok := cutAnnounce(text); ok {
		return SlashCommand{Type: TypeAnnounce, Color: color, Text: rest}, true
	}
	if rest, ok := cutVerb(text, "/shoutout"); ok {
		rest = strings.TrimLeft(rest, " ")
		target, remainder, _ := strings.Cut(rest, " ")
		return SlashCommand{
			Type: TypeShoutout,
			To:   strings.TrimPrefix(target, "@"),
			Text: strings.TrimLeft(remainder, " "),
		}, true
	}
	if rest, ok := cutVerb(text, "/pin"); ok {
		return SlashCommand{Type: TypePin, Text: rest}, true
	}
	return SlashCommand{}, false
}

func cutAnnounce(text string) (color, rest string, ok bool) {
	type variant struct {
		verb  string
		color string
	}
	for _, v := range []variant{
		{"/announceblue", "blue"},
		{"/announcegreen", "green"},
		{"/announceorange", "orange"},
		{"/announcepurple", "purple"},
		{"/announce", "primary"},
	} {
		if r, matched := cutVerb(text, v.verb); matched {
			return v.color, r, true
		}
	}
	return "", "", false
}

func cutVerb(text, verb string) (rest string, ok bool) {
	if text == verb {
		return "", true
	}
	if strings.HasPrefix(text, verb+" ") {
		return text[len(verb)+1:], true
	}
	return "", false
}
