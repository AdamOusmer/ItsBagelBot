// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package scope

import (
	"context"
	"strings"
)

// MaxPositional is the highest word a template may address: {30} and {30:}
// resolve, {31} does not and stays literal.
//
// Decision record. A cap is needed at all because the positional family is
// the one open-ended name space in the palette: without it Owns claims every
// integer, and "streaming since {2024}" would resolve to an empty word and
// vanish instead of staying visible as the literal a broadcaster can see and
// fix. So the question is only where to stop.
//
//   - unbounded: rejected for the reason above — a number nobody meant as a
//     token silently disappears, which is exactly the failure the
//     literal-passthrough rule exists to prevent.
//   - 9 (single digit): rejected. It keeps the parse to one byte, but it
//     makes the cap visible to broadcasters — a ten-word form command would
//     hit it — and the saving is one byte of scan.
//   - 30: a Twitch line is capped at 500 characters, so ~250 single-letter
//     words is the hard ceiling the platform already imposes; 30 sits far
//     enough above any template written by hand that the cap is invisible,
//     and keeps positionalIndex to a two-byte scan with no overflow to
//     reason about.
const MaxPositional = 30

// Message answers the tokens that are already on the chat line that triggered
// the command: who typed it, what they typed after the trigger, and where.
// Nothing here costs a lookup, so it is always mounted.
//
// The values arrive already sanitized (engine.sanitizeVar) — the viewer
// controls Args, Words and Touser, and a leading slash run in any of them
// would otherwise become a moderation verb once the reply is split per line.
type Message struct {
	// User and Sender are the same chatter under two spellings; both are
	// kept because commands written against either must keep working.
	User   string
	Sender string
	// Args is the rest of the line after the trigger.
	Args string
	// Words is Args split into the words {1}..{30} and {n:} address, each
	// sanitized on its own. It is a separate field rather than a split of
	// Args because the sanitizing differs: see engine.sanitizeWords.
	Words []string
	// Touser is the mentioned viewer, defaulting to the sender. {target} is
	// the dashboard-facing name for the same value.
	Touser string
	// Channel is the broadcaster's display name.
	Channel string
	// UserID and Login are the chatter's stable platform id and lower-case
	// login, both distinct from the display name {user} renders.
	UserID string
	Login  string
	// Command is the CANONICAL command name that matched, without its '!'.
	// An alias resolves to it, so {command} in a response shared by !hug and
	// !cuddle prints the one name the use counter also keys on.
	Command string
}

// messageFields maps every fixed name this scope owns to the field that
// answers it. It is a table rather than a switch so Owns and Get cannot
// drift: a name is in the palette exactly when it can be resolved.
//
// The positional names are not here — they are generated (see
// positionalIndex) rather than enumerated.
var messageFields = map[string]func(Message) string{
	"user":       func(m Message) string { return m.User },
	"sender":     func(m Message) string { return m.Sender },
	"args":       func(m Message) string { return m.Args },
	"touser":     func(m Message) string { return m.Touser },
	"target":     func(m Message) string { return m.Touser },
	"channel":    func(m Message) string { return m.Channel },
	"userid":     func(m Message) string { return m.UserID },
	"user.login": func(m Message) string { return m.Login },
	"command":    func(m Message) string { return m.Command },
}

// Owns claims the fixed identity/argument palette plus {1}..{30}.
func (Message) Owns(name string) bool {
	if _, ok := positionalIndex(name); ok {
		return true
	}
	_, ok := messageFields[name]
	return ok
}

// Plan hands the struct back: every value is already in hand by the time a
// command runs, so there is nothing to batch and no ctx to spend.
func (m Message) Plan(context.Context, []Var) (Values, error) {
	return m, nil
}

// Get resolves one message token.
//
// A payload on a fixed name is rejected rather than ignored: {user} is the
// token, {user:bob} is not one, and answering it as if the payload were
// absent would silently invent a grammar (and break the day a real {user:...}
// form ships). It stays literal, like every other name this palette does not
// have. The positional names are the one place a payload means something, and
// only the empty one does.
func (m Message) Get(v Var) (string, bool) {
	if n, ok := positionalIndex(v.Name); ok {
		return m.positional(n, v)
	}
	if v.HasPayload {
		return "", false
	}
	field, ok := messageFields[v.Name]
	if !ok {
		return "", false
	}
	return field(m), true
}

// positional answers the two positional spellings and nothing else: {n} is
// word n, {n:} is words n to the end. A word past the end resolves to the
// empty string (ok=true) so the span's fallback renders — "hug {1|everyone}"
// has to read as a sentence when nobody was named.
//
// {n:m} is deliberately absent. It would be a second grammar on the same
// name (a payload that is sometimes a bound and sometimes an end marker) for
// a slice nothing in the imported corpora asks for, so a non-empty payload
// stays literal and the spelling is free to mean something later.
func (m Message) positional(n int, v Var) (string, bool) {
	switch {
	case !v.HasPayload:
		return m.word(n), true
	case v.Payload == "":
		return m.rest(n), true
	default:
		return "", false
	}
}

// word is the n'th argument word, 1-based, or "" past the end.
func (m Message) word(n int) string {
	if n > len(m.Words) {
		return ""
	}
	return m.Words[n-1]
}

// rest is words n to the end, space-joined, or "" past the end.
//
// {1:} and {args} carry the same text for ordinary arguments but are NOT the
// same expression, and both are kept on purpose: {args} is the raw remainder
// with its whitespace intact, {1:} is the sanitized words rejoined, so a run
// of spaces collapses and a word that starts with '/' is defanged. That makes
// {1:} the safe spelling to put anywhere in a line and {args} the faithful
// one to put where the chatter's own text already begins.
func (m Message) rest(n int) string {
	if n > len(m.Words) {
		return ""
	}
	return strings.Join(m.Words[n-1:], " ")
}

// positionalIndex reads a name as a positional word number in 1..MaxPositional.
//
// It parses rather than looks up so the palette needs no thirty-entry table,
// and it is hand-rolled rather than strconv.Atoi because Atoi also accepts
// "+1" and "-1": {+1} is not a token anyone wrote, and accepting it would
// quietly claim a span that should stay literal. A leading zero is rejected
// for the same reason ({01} is not word 1), and two bytes is the whole range
// since MaxPositional has two digits, so "007" never reaches the loop.
func positionalIndex(name string) (int, bool) {
	if len(name) > 2 || strings.HasPrefix(name, "0") {
		return 0, false
	}
	n := 0
	for i := 0; i < len(name); i++ {
		if name[i] < '0' || name[i] > '9' {
			return 0, false
		}
		n = n*10 + int(name[i]-'0')
	}
	if n < 1 || n > MaxPositional {
		return 0, false
	}
	return n, true
}
