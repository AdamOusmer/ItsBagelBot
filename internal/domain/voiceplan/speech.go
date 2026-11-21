// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package voiceplan

import (
	"strings"
	"unicode"
)

const (
	SpeechChat   AlertKind = "chat"
	SpeechReward AlertKind = "reward"
	SpeechRecipe AlertKind = "recipe"
)

const (
	ChatCommand             = "tts"
	ChatDefaultUserCooldown = 30
	ChatDefaultMaxChars     = 200
	DefaultPrefixName       = true
	DefaultSpeechStyle      = "warm"
	DefaultRewardResolution = "fulfill"
)

var speechKinds = map[AlertKind]struct{}{SpeechChat: {}, SpeechReward: {}, SpeechRecipe: {}}

func (kind AlertKind) Valid() bool {
	_, alert := alertDefaults[kind]
	_, speech := speechKinds[kind]
	return alert || speech
}

var angleBrackets = strings.NewReplacer("<", " ", ">", " ")

func StripControls(text string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case unicode.IsControl(r) && unicode.IsSpace(r):
			return ' '
		case unicode.IsControl(r), unicode.Is(unicode.Cf, r):
			return -1
		default:
			return r
		}
	}, text)
}

func PlainText(value string) string {
	value = angleBrackets.Replace(StripControls(value))
	return strings.Join(strings.Fields(value), " ")
}

func HasSpokenContent(text string) bool {
	return strings.ContainsFunc(text, func(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) })
}

func TrimRunes(text string, limit int) string {
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return strings.TrimSpace(string(runes[:max(limit, 0)]))
}
