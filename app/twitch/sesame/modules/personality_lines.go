// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import "ItsBagelBot/internal/domain/i18n"

// The English catalog defines the size and order of each reply pack. Mood
// values stay in English in the store so a locale change mid-stream preserves
// the same mood; the selected line is translated when it is shown.
var (
	personalityGoodPack      = i18n.DefaultSeries("personality.good")
	personalityBadPack       = i18n.DefaultSeries("personality.bad")
	personalityGiveBagel     = i18n.DefaultSeries("personality.give")
	personalityThanksPack    = i18n.DefaultSeries("personality.thanks")
	personalityAffectionPack = i18n.DefaultSeries("personality.affection")
	personalityFeedCountPack = i18n.DefaultSeries("personality.feed")
	personalityBoopPack      = i18n.DefaultSeries("personality.boop")
	personalityGnPack        = i18n.DefaultSeries("personality.gn")
	personalityMoodPack      = i18n.DefaultSeries("personality.mood")
	personalityEmojiPack     = i18n.DefaultSeries("personality.emoji")
	personalityToastLines    = i18n.DefaultSeries("personality.toast")
	personalityFacts         = i18n.DefaultSeries("personality.fact")
)
