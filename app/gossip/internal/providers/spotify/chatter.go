// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package spotify

import (
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

func (raw matchText) withoutEmotes() matchText {
	words := strings.Fields(string(raw))
	if len(words) < 2 {
		return raw
	}
	end := len(words)
	for end > 1 && emoteShape(words[end-1]) {
		end--
	}
	if end == len(words) {
		return raw
	}
	return matchText(strings.Join(words[:end], " "))
}

func (raw matchText) emoteTitleOnly(clean matchText) bool {
	removed := strings.Fields(string(raw)[len(clean):])
	if slices.ContainsFunc(removed, camelCase) {
		return true
	}
	if !slices.ContainsFunc(strings.Fields(string(clean)), ordinaryChatWord) {
		return false
	}
	return len(removed) > 1 || utf8.RuneCountInString(removed[0]) > 3
}

func ordinaryChatWord(word string) bool {
	return !emoteShape(word)
}

func emoteShape(word string) bool {
	if strings.ContainsFunc(word, nonLetter) {
		return false
	}
	if utf8.RuneCountInString(word) >= 3 && strings.ToUpper(word) == word {
		return strings.ContainsFunc(word, unicode.IsUpper)
	}
	return camelCase(word)
}

func nonLetter(r rune) bool { return !unicode.IsLetter(r) }

func camelCase(word string) bool {
	previousLower := false
	for _, r := range word {
		if previousLower && unicode.IsUpper(r) {
			return true
		}
		previousLower = unicode.IsLower(r)
	}
	return false
}

func textCacheKey(raw matchText) string {
	key := normalizeText(string(raw))
	clean := raw.withoutEmotes()
	alternative := normalizeText(string(clean))
	if alternative != key {
		return key + "\x00" + strconv.FormatBool(raw.emoteTitleOnly(clean)) + "\x00" + alternative
	}
	return key
}

func chatContent(s matchText) matchText {
	words := strings.Fields(string(s))
	if len(words) == 1 && matchText(words[0]).webToken() {
		return s
	}
	var kept []string
	for _, word := range words {
		if !discardChatWord(chatWord{text: matchText(word), first: len(kept) == 0}) {
			kept = append(kept, word)
		}
	}
	return matchText(strings.Join(kept, " "))
}

type chatWord struct {
	text  matchText
	first bool
}

func discardChatWord(word chatWord) bool {
	return word.text.chatClutter() || word.first && chatCommand.MatchString(string(word.text))
}

func (word matchText) chatClutter() bool {
	return strings.HasPrefix(string(word), "@") || word == "<3" || word.webToken()
}

func (word matchText) webToken() bool {
	lower := strings.ToLower(string(word))
	ref := inputReference{lower: lower}
	return ref.hasWebScheme() || ref.hasBareSpotifyHost() || strings.HasPrefix(lower, "//")
}

var chatCommand = regexp.MustCompile(`^![[:alnum:]_]+$`)
