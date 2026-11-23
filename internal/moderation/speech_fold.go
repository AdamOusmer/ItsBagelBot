// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package moderation

import (
	"strings"
)

const repeatKeep = 2

var innocentCollisions = map[string]bool{"niger": true, "fagot": true}

func foldVariants(text string) [][]byte {
	words := strings.Fields(speechKey(text))
	collapsed := mapWords(words, collapseRepeats)
	reduced := mapWords(collapsed, reduceRepeats)
	return [][]byte{paddedWords(collapsed), paddedWords(reduced)}
}

func paddedWords(words []string) []byte {
	return []byte(" " + strings.Join(words, " ") + " ")
}

func mapWords(words []string, fn func(string) string) []string {
	out := make([]string, len(words))
	for i, word := range words {
		out[i] = fn(word)
	}
	return out
}

func collapseRepeats(word string) string { return limitRuns(word, repeatKeep) }

func reduceRepeats(word string) string {
	reduced := limitRuns(word, 1)
	if innocentCollisions[reduced] {
		return word
	}
	return reduced
}

func limitRuns(word string, keep int) string {
	var b strings.Builder
	var prev rune
	run := 0
	for _, r := range word {
		if r == prev {
			run++
		} else {
			prev, run = r, 1
		}
		if run <= keep {
			b.WriteRune(r)
		}
	}
	return b.String()
}
