// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"strconv"

	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/internal/domain/i18n"
	"ItsBagelBot/internal/domain/validate"
	"ItsBagelBot/internal/moderation"
)

type RaffleAnnouncement struct {
	Locale string
}

func floorHit(text string) bool {
	_, hit := moderation.CheckFloor(text)
	return hit
}

func (a RaffleAnnouncement) render(override, key string, kv ...string) string {
	if text := a.expand(override, kv); override != "" && !floorHit(text) {
		return text
	}
	return a.expand(i18n.T(a.Locale, key), kv)
}

func (a RaffleAnnouncement) expand(text string, kv []string) string {
	if text == "" {
		return ""
	}
	return expandTokens(module.Locale(a.Locale), tokenExpansion{namespace: "raffle", text: text, kv: kv})
}

func chunkMentions[T any](winners []T, login func(T) string, render func(targets string) string) []string {
	return chunkJoinedMentions(winners, mentionFormat[T]{login: login, render: render, separator: ", "})
}

type mentionFormat[T any] struct {
	login     func(T) string
	render    func(string) string
	separator string
}

func chunkJoinedMentions[T any](winners []T, format mentionFormat[T]) []string {
	var lines []string
	group := ""
	for _, w := range winners {
		name := format.login(w)
		if name == "" {
			continue
		}
		mention := "@" + name
		next := mention
		if group != "" {
			next = group + format.separator + mention
		}
		if group != "" && len(format.render(next)) > validate.MaxResponseLineLength {
			lines = append(lines, format.render(group))
			next = mention
		}
		group = next
	}
	if group == "" {
		return nil
	}
	return append(lines, format.render(group))
}

func (a RaffleAnnouncement) RemindText(leftSeconds, entrants int64) string {
	return a.render("", "raffle.remind",
		"mins", strconv.FormatInt((leftSeconds+59)/60, 10),
		"count", strconv.FormatInt(entrants, 10))
}
