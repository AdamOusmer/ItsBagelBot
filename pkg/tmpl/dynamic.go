// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package tmpl

import (
	"math/rand/v2"
	"strconv"
	"strings"
)

// Dynamic evaluates the two spans that need nothing but the span itself:
// {random}, {random:min-max} and {choice:a,b,c}. It is the fallthrough a
// reply-template resolver reaches for after its own palette missed, so a
// broadcaster can write a dice roll or a coin flip into any customizable line.
//
// It used to live in sesame's module package as ParseDynamic and re-derived
// the name and the payload from the "name:payload" key string it was handed —
// a second reader of the span grammar, one HasPrefix/TrimPrefix pair per
// token family. It reads the lexed Token now, so it cannot disagree with the
// lexer about where a name ends, and it lives in pkg/tmpl so that outgress
// (which must not import sesame, see internal/buildguard) gets the same two
// tokens rather than a hand-written near-copy.
//
// Unknown names answer ok=false, which leaves the span literal — a typo stays
// visible to the broadcaster who wrote it, per Token.Resolve.
func Dynamic(tok Token) (val string, ok bool) {
	switch tok.Name {
	case dynamicRandom:
		return randomValue(tok)
	case dynamicChoice:
		return choiceValue(tok)
	default:
		return "", false
	}
}

const (
	dynamicRandom = "random"
	dynamicChoice = "choice"

	// randomDefaultMax is the ceiling of the payload-free {random}: 1..100,
	// the percentile roll every chat bot in this space spells the same way.
	randomDefaultMax = 100
)

// randomValue rolls {random} (1..100) or {random:min-max}.
//
// A payload that is not a well-formed range answers ok=false rather than
// falling back to the 1..100 roll: {random:1..6} is a typo, and printing a
// plausible number for it would hide the mistake forever, where the literal
// "{random:1..6}" in chat names it.
func randomValue(tok Token) (string, bool) {
	if !tok.HasPayload {
		return strconv.Itoa(rand.IntN(randomDefaultMax) + 1), true
	}
	low, high, ok := rangeBounds(tok.Payload)
	if !ok {
		return "", false
	}
	return strconv.Itoa(rand.IntN(high-low+1) + low), true
}

// rangeBounds reads "min-max" out of a {random:…} payload. The split is at the
// FIRST '-', which is why a negative minimum ("-5-5") does not parse: that has
// always been the behaviour, and widening it is a grammar change this refactor
// deliberately does not make.
func rangeBounds(payload string) (low, high int, ok bool) {
	lowText, highText, found := strings.Cut(payload, "-")
	if !found {
		return 0, 0, false
	}
	low, err := strconv.Atoi(lowText)
	if err != nil {
		return 0, 0, false
	}
	high, err = strconv.Atoi(highText)
	if err != nil {
		return 0, 0, false
	}
	if high < low {
		return 0, 0, false
	}
	return low, high, true
}

// choiceValue draws one option out of {choice:a,b,c}.
//
// The two edge spellings are pinned deliberately and differ:
//
//   - {choice} carries no payload, names no options, and stays LITERAL — the
//     author wrote half a token.
//   - {choice:} carries an empty payload, which is a one-element list holding
//     the empty string, and resolves to "" (so a fallback, {choice:|none},
//     applies). It is the same rule {2} vs {2:} follows in the golden table.
func choiceValue(tok Token) (string, bool) {
	if !tok.HasPayload {
		return "", false
	}
	// strings.Split never returns an empty slice, so the draw is always in
	// range: "" splits to [""], "a,b" to ["a","b"].
	options := strings.Split(tok.Payload, ",")
	return options[rand.IntN(len(options))], true
}

// NormalizeName folds a payload that names a stored thing — a counter, a
// urlfetch definition — the way its store folds it: trim, drop one leading
// '!', trim again, lower-case.
//
// It lives beside the lexer rather than in the sesame scope package that grew
// it because the fold IS part of the token grammar: "{COUNTER:Deaths}" and
// "{counter: !deaths }" have to be one bump, not two, and the surfaces that
// need the same answer are not all allowed to import sesame. The command
// repository (app/db) resolves "which commands reference this definition"
// with it, and engine.NormalizeCounterName and scope.NormalizeName delegate
// here, so no two of them can answer differently.
func NormalizeName(name string) string {
	return strings.ToLower(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(name), "!")))
}
