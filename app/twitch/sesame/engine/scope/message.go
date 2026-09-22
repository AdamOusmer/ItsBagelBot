// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package scope

import (
	"context"
	"net/url"
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
// controls Words and Touser, and a leading slash run in either would
// otherwise become a moderation verb once the reply is split per line.
type Message struct {
	// User and Sender are the same chatter under two spellings; both are
	// kept because commands written against either must keep working.
	User   string
	Sender string
	// Words is the argument line split into the words {1}..{30}, {n:} and
	// {n:m} address, each sanitized on its own — see engine.sanitizeWords.
	// {args} and {querystring} are also derived from it (m.rest(1)), so a
	// slash-verb hiding in word 3 cannot reach either by skipping the split.
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

// messageFields maps every fixed CANONICAL name this scope owns to the field
// that answers it. It is a table rather than a switch so Owns and Get cannot
// drift: a name is in the palette exactly when it can be resolved.
//
// The positional names and the empty name ({:N}) are not here — they are
// generated (see positionalIndex and Get) rather than enumerated. Nor are the
// superseded spellings in messageAliases: CommandTokenFamilies builds the
// message family's example list straight from this map, and the whole point
// of an alias is that it no longer needs teaching.
var messageFields = map[string]func(Message) string{
	"user":   func(m Message) string { return m.User },
	"sender": func(m Message) string { return m.Sender },
	// {args} is {1:} — the same expression, not merely the same text for
	// ordinary input. It used to carry the raw remainder untouched (whatever
	// whitespace the chatter typed, a mid-line '/' left alone), and {1:} the
	// sanitized words rejoined; that was two behaviours under one spelling,
	// which cost more authoring confusion than the raw form ever bought.
	// Dropping it collapses runs of whitespace and defangs every word, not
	// just the first — measured as no import target or saved command in the
	// corpora relies on whitespace runs surviving {args}.
	"args":       func(m Message) string { return m.rest(1) },
	"touser":     func(m Message) string { return m.Touser },
	"target":     func(m Message) string { return m.Touser },
	"channel":    func(m Message) string { return m.Channel },
	"user.id":    func(m Message) string { return m.UserID },
	"user.login": func(m Message) string { return m.Login },
	"command":    func(m Message) string { return m.Command },
	// {querystring} is {args}, URL-encoded. It is a thin alias of the
	// {queryescape:…} encoder (scope.Pure), never a second one: the two must
	// escape the same bytes the same way, because the whole point of the
	// spelling is pasting it inside a saved {urlfetch:…} URL.
	//
	// It lives here rather than in the pure scope because its INPUT is the
	// chat line, not the span: a pure token is a function of its own payload,
	// and this one reads the arguments. That also makes it the token every
	// importer wants — Nightbot's $(querystring) is exactly this — while
	// {queryescape:…} stays the general form for literal text.
	"querystring": func(m Message) string { return url.QueryEscape(m.rest(1)) },
}

// messageAliases folds a superseded spelling onto the canonical name that now
// answers it, so a command saved against the old spelling keeps resolving
// without teaching it beside the canonical one: token_catalog.go's examples
// (and the guide page built from them) show only messageFields' keys.
var messageAliases = map[string]string{
	"userid": "user.id",
}

// canonicalName folds an alias onto the name messageFields is keyed by,
// leaving every other name — including one messageAliases has no entry for
// — untouched.
func canonicalName(name string) string {
	if canon, ok := messageAliases[name]; ok {
		return canon
	}
	return name
}

// Owns claims the fixed identity/argument palette, {1}..{30}, and the empty
// name ({:N} slices to word N — see Get). A payload on the empty name that
// is not a valid positional bound is still OWNED here rather than left to a
// later scope (none would claim "" anyway), and Get is what turns that into
// a literal.
func (Message) Owns(name string) bool {
	if _, ok := positionalIndex(name); ok {
		return true
	}
	if name == "" {
		return true
	}
	_, ok := messageFields[canonicalName(name)]
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
// have. The positional and empty names are the one place a payload means
// something.
func (m Message) Get(v Var) (string, bool) {
	if n, ok := positionalIndex(v.Name); ok {
		return m.positional(n, v)
	}
	if v.Name == "" {
		return m.leadingSlice(v)
	}
	if v.HasPayload {
		return "", false
	}
	field, ok := messageFields[canonicalName(v.Name)]
	if !ok {
		return "", false
	}
	return field(m), true
}

// The positional grammar, all four shapes read off {n} and its payload:
//
//	{n}    word n
//	{n:}   words n..end
//	{:m}   words 1..m   (m via the empty name, Get's leadingSlice)
//	{n:m}  words n..m, inclusive
//
// Every bound is a positionalIndex (1..MaxPositional, no sign, no leading
// zero). A bound past the end of Words clamps to the end; a START past the
// end resolves to "" (ok=true) so the span's fallback renders — "hug
// {1|everyone}" has to read as a sentence when nobody was named. m < n is an
// author error, not a clamp: {3:1} is not "word 3 to nowhere", it is a typo
// worth leaving visible, so it stays literal like a name this palette does
// not have.

// positional answers {n} and {n:m} (including the {n:} case, m="").
func (m Message) positional(n int, v Var) (string, bool) {
	switch {
	case !v.HasPayload:
		return m.word(n), true
	case v.Payload == "":
		return m.rest(n), true
	default:
		return m.boundedSlice(n, v.Payload)
	}
}

// leadingSlice answers {:m}: the empty name with a numeric payload is words
// 1..m, same rules as {n:m}. Every other empty-name span — bare {}, {:},
// {:x} — has no positional to generate and stays literal.
func (m Message) leadingSlice(v Var) (string, bool) {
	if !v.HasPayload {
		return "", false
	}
	end, ok := positionalIndex(v.Payload)
	if !ok {
		return "", false
	}
	return m.slice(1, end), true
}

// boundedSlice answers the payload half of {n:m}: a non-numeric or
// out-of-range m stays literal (nonsense, not a slice), and m < n is the
// author-error case the type comment above explains.
func (m Message) boundedSlice(n int, payload string) (string, bool) {
	end, ok := positionalIndex(payload)
	if !ok || end < n {
		return "", false
	}
	return m.slice(n, end), true
}

// word is the n'th argument word, 1-based, or "" past the end.
func (m Message) word(n int) string {
	if n > len(m.Words) {
		return ""
	}
	return m.Words[n-1]
}

// rest is words n to the end, space-joined, or "" past the end.
func (m Message) rest(n int) string {
	return m.slice(n, len(m.Words))
}

// slice is words n..end inclusive, space-joined. n past the end of Words
// renders "" (the caller's ok stays true, so a fallback fires); end past the
// end clamps to the last word rather than erroring, since {2:30} — "word 2
// to whatever's left" — is the common shape of an open-ended {n:}-style
// request, not a typo the way end < n is.
func (m Message) slice(n, end int) string {
	if n > len(m.Words) {
		return ""
	}
	if end > len(m.Words) {
		end = len(m.Words)
	}
	return strings.Join(m.Words[n-1:end], " ")
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
