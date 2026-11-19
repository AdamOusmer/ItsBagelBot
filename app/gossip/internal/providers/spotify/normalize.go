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

type matchPhrase []string
type matchText string
type wordIndex int
type foldedLetters []rune
type smallNumber int

func (p matchPhrase) text() string    { return strings.Join(p, " ") }
func (p matchPhrase) compact() string { return strings.Join(p, "") }

func (p matchPhrase) runeCount() int {
	n := 0
	for _, word := range p {
		n += utf8.RuneCountInString(word)
	}
	return n
}

func (p matchPhrase) withoutArticle() matchPhrase {
	if len(p) > 1 && p[0] == "the" {
		return p[1:]
	}
	return p
}

var keycap = regexp.MustCompile(`[#*0-9][\x{FE0E}\x{FE0F}]?\x{20E3}`)
var letterFolds = strings.NewReplacer("ø", "o", "æ", "ae", "œ", "oe", "ß", "ss", "ł", "l", "đ", "d", "ı", "i", "þ", "th")
var chatWords = map[string]string{"u": "you", "ur": "your", "r": "are", "n": "and"}

func matchWords(s string) matchPhrase {
	return normalizeWords(strings.Fields(matchText(s).tokens(false, false).text))
}

func (letters foldedLetters) hasWord() bool { return slices.ContainsFunc(letters, wordRune) }

type letterFold struct{ base rune }

func (f *letterFold) rune(r rune) rune {
	switch {
	case r == '\'', r == '’':
		return -1
	case unicode.IsMark(r):
		if !unicode.IsLetter(f.base) || unicode.In(f.base, unicode.Latin, unicode.Greek, unicode.Cyrillic) {
			return -1
		}
		return r
	default:
		f.base = r
		return unicode.ToLower(r)
	}
}

func wordRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsNumber(r)
}

var stylizedLetters = map[rune]string{'!': "i", '$': "s", '@': "a"}

func (letters foldedLetters) tokenAt(i wordIndex, lookup bool) string {
	r := letters[i]
	switch {
	case wordRune(r), unicode.IsMark(r):
		return string(r)
	case lookup:
		return " "
	case r == '+', r == '&':
		return " and "
	case stylizedLetters[r] != "" && letters.betweenLetters(i):
		return stylizedLetters[r]
	default:
		return " "
	}
}

func (letters foldedLetters) betweenLetters(i wordIndex) bool {
	if i == 0 || int(i)+1 == len(letters) {
		return false
	}
	return unicode.IsLetter(letters[i-1]) && unicode.IsLetter(letters[i+1])
}

var smallNumbers = [...]string{
	"zero", "one", "two", "three", "four", "five", "six", "seven", "eight", "nine",
	"ten", "eleven", "twelve", "thirteen", "fourteen", "fifteen", "sixteen", "seventeen", "eighteen", "nineteen",
}
var tens = [...]string{"", "", "twenty", "thirty", "forty", "fifty", "sixty", "seventy", "eighty", "ninety"}

var numberTokens = func() map[string]bool {
	words := make(map[string]bool)
	for _, word := range smallNumbers {
		words[word] = true
	}
	for _, word := range tens[2:] {
		words[word] = true
	}
	return words
}()

func normalizeWords(words matchPhrase) matchPhrase {
	out := make(matchPhrase, 0, len(words))
	for i := range words {
		out = append(out, words.normalizedToken(wordIndex(i))...)
	}
	return out.withoutArticle()
}

func (words matchPhrase) normalizedToken(i wordIndex) matchPhrase {
	word := words[i]
	if n, err := strconv.Atoi(word); err == nil && n < 100 {
		return smallNumber(n).words()
	}
	if replacement := chatWords[word]; replacement != "" && !words.inInitialism(i) {
		return matchPhrase{replacement}
	}
	return matchPhrase{word}
}

func (words matchPhrase) inInitialism(i wordIndex) bool {
	for start := max(0, int(i)-2); start <= int(i); start++ {
		if start+3 <= len(words) && !slices.ContainsFunc(words[start:start+3], nonInitial) {
			return true
		}
	}
	return false
}

func nonInitial(word string) bool { return !isInitial(word) }

func isInitial(word string) bool {
	r, size := utf8.DecodeRuneInString(word)
	return size == len(word) && unicode.IsLetter(r)
}

func (n smallNumber) words() matchPhrase {
	switch {
	case int(n) < len(smallNumbers):
		return matchPhrase{smallNumbers[n]}
	case n%10 == 0:
		return matchPhrase{tens[n/10]}
	default:
		return matchPhrase{tens[n/10], smallNumbers[n%10]}
	}
}

var leadingFiller = map[string]bool{
	"play": true, "pls": true, "plz": true, "please": true, "sr": true, "add": true,
	"can you play": true, "could you play": true, "please play": true, "pls play": true,
}

var trailingFiller = map[string]bool{
	"pls": true, "plz": true, "please": true, "song": true, "thanks": true, "thx": true, "ty": true, "lol": true, "next": true,
}

type queryVariant struct {
	words     matchPhrase
	titleOnly bool
}

func (p matchPhrase) contentRange() wordRange {
	first, last := 0, len(p)
	for n := p.leadingFillerLength(); n > 0; n = p[first:].leadingFillerLength() {
		first += n
	}
	for last > first && trailingFiller[p[last-1]] {
		last--
	}
	return wordRange{first, last}
}

var artistRequiredFiller = map[string]bool{"play": true, "add": true, "song": true, "next": true}
var politeRequestEndings = map[string]bool{"pls": true, "plz": true, "please": true}

func fillerAllowsTitle(leading, trailing matchPhrase) bool {
	if slices.Equal(leading, matchPhrase{"play"}) && len(trailing) == 1 {
		return politeRequestEndings[trailing[0]]
	}
	return len(leading) <= 1 && !leading.requiresArtist() && !trailing.requiresArtist()
}

func (p matchPhrase) requiresArtist() bool {
	for _, word := range p {
		if artistRequiredFiller[word] {
			return true
		}
	}
	return false
}

func (p matchPhrase) leadingFillerLength() int {
	for n := min(len(p), 3); n > 0; n-- {
		if leadingFiller[p[:n].text()] {
			return n
		}
	}
	return 0
}

type wordRange struct{ start, end int }

func (r wordRange) words(words matchPhrase) matchPhrase { return words[r.start:r.end] }

func (r wordRange) isTrimmed(length int) bool {
	size := r.end - r.start
	return size > 0 && size < length
}
