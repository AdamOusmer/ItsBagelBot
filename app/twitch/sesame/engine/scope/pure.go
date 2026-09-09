// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package scope

import (
	"context"
	"net/url"
	"strconv"
	"strings"
	"time"

	"ItsBagelBot/pkg/tmpl"
)

// Pure answers the tokens that depend on nothing outside the span itself:
// the dice ({random}, {random:min-max}, {choice:a,b,c}) and the utilities
// that are a function of their own payload ({math:…}, {queryescape:…},
// {pathescape:…}, {repeat:n:phrase}, {countdown:…}, {countup:…}).
//
// It is always mounted — there is no dependency to be missing — and is first
// in the chain so a later scope can never shadow the dice.
//
// The utilities are NOT part of tmpl.Dynamic, and that split is deliberate:
// Dynamic is the fallback palette every module REPLY template resolves
// against (alerts, rewards, trigger words), and those templates are one line
// of copy, not a place to compute. Keeping the utilities in the command scope
// means "the palette a custom command has" is one list in one place, and a
// module reply keeps the small, obvious palette it always had.
type Pure struct {
	// Now is the clock {countdown}/{countup} measure against. A nil Now means
	// time.Now; a test pins it so a rendered span is an assertion rather than
	// a race with the wall clock.
	Now func() time.Time
	// Locale is the broadcaster's console language, used to word the
	// countdown spans through the shared humanizer. The zero value is the
	// catalog's English fallback, which is what an unconfigured channel gets
	// everywhere else too.
	Locale string
}

// pureUtils maps each utility name to the function of its payload that
// answers it. It is a table rather than a switch so Owns and Get cannot
// drift: a name is in the palette exactly when it can be resolved.
var pureUtils = map[string]func(Pure, string) string{
	"math":        func(_ Pure, payload string) string { return evalMath(payload) },
	"queryescape": func(_ Pure, payload string) string { return url.QueryEscape(payload) },
	"pathescape":  func(_ Pure, payload string) string { return url.PathEscape(payload) },
	"repeat":      func(_ Pure, payload string) string { return repeatPhrase(payload) },
	"countdown":   Pure.countdownUtil,
	"countup":     Pure.countupUtil,
}

// countdownUtil and countupUtil exist only to give the two clock helpers the
// table's func(Pure, string) shape.
func (p Pure) countdownUtil(payload string) string { return p.countdown(payload) }
func (p Pure) countupUtil(payload string) string   { return p.countup(payload) }

// Owns claims the two generic dynamic names tmpl.Dynamic answers, plus
// the payload utilities above.
func (Pure) Owns(name string) bool {
	if name == "random" || name == "choice" {
		return true
	}
	_, ok := pureUtils[name]
	return ok
}

// Plan does no work: a pure token needs no ctx, no batching and no lookup.
func (p Pure) Plan(context.Context, []Var) (Values, error) {
	return pureValues{p: p}, nil
}

// pureValues evaluates each span at RENDER time rather than caching a value
// per key in Plan.
//
// That is deliberate and is the one place a scope's Values is not a map:
// "{random} and {random}" must print two different numbers, the way it always
// has and the way anyone writing a dice command expects. Resolving in Plan
// would key both spans on "random" and print the same number twice, turning a
// visible feature into a silent one-line regression. The cost is nil — there
// is no I/O to hoist out of the render phase here, which is exactly what
// makes the scope pure.
type pureValues struct{ p Pure }

func (v pureValues) Get(tok Var) (string, bool) {
	util, ok := pureUtils[tok.Name]
	if !ok {
		return tmpl.Dynamic(tok)
	}
	// A utility with no payload names nothing to work on: {math} is not the
	// token, {math:1+1} is. It stays literal (ok=false) like any other name
	// the palette does not have, rather than resolving to "" — so a
	// broadcaster who typed the wrong spelling sees the braces in chat.
	if !tok.HasPayload {
		return "", false
	}
	return util(v.p, tok.Payload), true
}

// Repeat caps. Decision record.
//
// A command template is authored by the broadcaster, not typed by a viewer,
// so nothing in {repeat:n:phrase} is untrusted input and the payload does not
// go through sanitizeVar (that guard exists for {args} and friends, which a
// viewer controls). The caps are therefore NOT a trust boundary — they are a
// platform one:
//
//   - Twitch drops a chat line over 500 characters. Without a byte cap,
//     {repeat:20:some fairly long sentence} produces a message the platform
//     silently refuses, which reads to the broadcaster as "the bot is broken"
//     rather than "that line is too long".
//   - MaxRepeatCount bounds the count independently of the byte cap so the
//     failure is legible while writing the template: a count over 20 is
//     rejected outright, whatever the phrase, instead of depending on how
//     long today's phrase happens to be.
//
// 480 rather than 500 leaves room for the rest of the template around the
// repeated run: a repeat that exactly fills the line has no space for the
// words either side of it, which is how every real use writes it.
const (
	MaxRepeatCount = 20
	MaxRepeatBytes = 480
)

// repeatPhrase renders "{repeat:n:phrase}" as the phrase n times, space
// separated, or "" when the payload is not that shape or would blow a cap.
func repeatPhrase(payload string) string {
	countText, phrase, ok := strings.Cut(payload, ":")
	if !ok || phrase == "" {
		return ""
	}
	n, ok := repeatCount(countText)
	if !ok || n*len(phrase)+n-1 > MaxRepeatBytes {
		return ""
	}
	parts := make([]string, n)
	for i := range parts {
		parts[i] = phrase
	}
	return strings.Join(parts, " ")
}

// repeatCount reads the count, digits only: strconv.Atoi would also accept
// "+3" and "-3", and a sign is not part of a spelling anyone writes here.
// A count outside 1..MaxRepeatCount is refused, so {repeat:0:hi} renders its
// fallback rather than an empty run that looks like a lost phrase.
func repeatCount(text string) (int, bool) {
	if text == "" || strings.TrimLeft(text, "0123456789") != "" {
		return 0, false
	}
	n, err := strconv.Atoi(text)
	if err != nil || outsideRepeatRange(n) {
		return 0, false
	}
	return n, true
}

// outsideRepeatRange names the bound {repeat:n:…} holds to: at least one copy,
// and at most MaxRepeatCount so a template cannot mint an unbounded run.
func outsideRepeatRange(n int) bool {
	return n < 1 || n > MaxRepeatCount
}
