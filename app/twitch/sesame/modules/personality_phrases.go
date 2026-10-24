// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import "strings"

// normalizedText is normalizeChat output: word runes joined by single spaces.
type normalizedText string

// phraseIndex answers word-boundary matching on normalized text by lookup: a
// phrase occurs at word edges exactly when it equals a run of consecutive words.
type phraseIndex struct {
	reaction map[normalizedText]int
	maxWords int
}

var personalityPhrases = indexPhrases(personalityReactions)

func indexPhrases(rs []reaction) phraseIndex {
	idx := phraseIndex{reaction: make(map[normalizedText]int)}
	for i, r := range rs {
		if r.matchRaw {
			continue
		}
		for _, p := range r.phrases {
			idx.add(normalizedText(p), i)
		}
	}
	return idx
}

func (idx *phraseIndex) add(phrase normalizedText, reaction int) {
	if phrase == "" || normalizeChat(string(phrase)) != string(phrase) {
		return
	}
	if _, claimed := idx.reaction[phrase]; !claimed {
		idx.reaction[phrase] = reaction
	}
	idx.maxWords = max(idx.maxWords, strings.Count(string(phrase), " ")+1)
}

// first returns the lowest reaction index with a phrase in norm, or -1.
func (idx phraseIndex) first(norm normalizedText) int {
	best := -1
	for start := 0; start < len(norm); {
		best = idx.bestFrom(norm[start:], best)
		next := strings.IndexByte(string(norm[start:]), ' ')
		if next < 0 {
			break
		}
		start += next + 1
	}
	return best
}

func (idx phraseIndex) bestFrom(text normalizedText, best int) int {
	end := 0
	for range idx.maxWords {
		next := strings.IndexByte(string(text[end:]), ' ')
		if next < 0 {
			return idx.better(text, best)
		}
		end += next
		best = idx.better(text[:end], best)
		end++
	}
	return best
}

func (idx phraseIndex) better(run normalizedText, best int) int {
	i, ok := idx.reaction[run]
	if !ok {
		return best
	}
	return lowerReaction(best, i)
}

func lowerReaction(best, candidate int) int {
	if best < 0 {
		return candidate
	}
	return min(best, candidate)
}
