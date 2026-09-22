// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package module

import (
	"ItsBagelBot/app/twitch/sesame/engine/scope"
	"ItsBagelBot/pkg/tmpl"
)

// Expand performs a single-pass {token} substitution over s, appending the
// result into dst and returning the grown slice. It allocates nothing of its
// own: the caller passes a pooled scratch buffer (see GetBuf) as dst.
//
// It is a thin delegation to pkg/tmpl, which owns the scanner and documents
// its brace, case and unknown-token rules. The name stays here because every
// module and the engine's own variable expansion speak in terms of it.
//
// repl is handed the lexed tmpl.Token, not a key string: a module that wants
// the whole "name:payload" lookup calls tok.Key(), and one that wants the
// payload alone reads tok.Payload instead of splitting the key back apart.
// That is the point of the type — no surface outside pkg/tmpl re-parses a
// span.
//
// This closure-taking form is the low-level primitive pkg/tmpl exposes; most
// reply surfaces now go through Palette.Expand/ExpandString instead (below),
// which is the one form that also knows the pure family ({math}, {countdown},
// …). Expand/ExpandString stay for the handful of callers — raffle mention
// lists, the vars_test.go golden-behaviour pins — that resolve against a
// bespoke closure rather than a declared Palette.
func Expand(dst []byte, s string, repl func(tok tmpl.Token) (val string, ok bool)) []byte {
	return tmpl.Append(dst, s, repl)
}

// ExpandString wraps Expand for callers who do not pool their own buffers,
// returning a newly allocated string.
func ExpandString(s string, repl func(tok tmpl.Token) (val string, ok bool)) string {
	return tmpl.Expand(s, repl)
}

// pureFallback answers the family every reply template gets for free once a
// palette itself misses: the dice ({random}, {choice:…}) and the payload
// utilities ({math}, {countdown}, {countup}, {repeat}, {queryescape},
// {pathescape}) documented at engine/scope/pure.go. Routing every palette's
// miss path through it — rather than tmpl.Dynamic, which only knows the dice
// — is what makes "the pure family works in module replies exactly like it
// does in custom commands" one line instead of a per-callsite change:
// pureValues.Get already falls through to tmpl.Dynamic for anything Pure does
// not own, so this is a strict superset of the old fallback, never a
// narrower one.
//
// No locale parameter: TokenExpander and StringPalette (the "stats family",
// see the decision record on TokenExpander below) carry no locale field to
// read one from — their reply structs are decoded once and passed around
// with no *module.Context alongside — so {countdown}/{countup} words in the
// catalog's English fallback there regardless of the channel's console
// language. That is a real, currently-accepted gap: fixing it means adding
// locale to every StringPalette-building function's signature (mcsr_ranked.go
// has it in scope at the call site; the TokenExpander literals in urchin.go
// and fortnite.go do not, without threading it through externalCommand too),
// which is the same >300-line churn the escape hatch on TokenExpander avoids
// for values. module.Palette (below) is where locale threading actually
// landed: every Palette-based reply — which is every reply that is NOT part
// of the stats family — carries its channel's locale through Common,
// Spec.Bind + WithLocale, or KV(…).WithLocale(c.Locale).
func pureFallback(tok tmpl.Token) (string, bool) {
	values, _ := scope.Pure{}.Plan(nil, nil)
	return values.Get(tok)
}

// TokenExpander maps a template token name to the accessor that renders it
// from one reply type. Every module that answers a chat command from an
// upstream reply had written the same closure — look the key up in my map,
// else fall through to the dynamic vars — so the palette now carries the
// expansion instead of each caller re-deriving it.
//
// It is keyed on *R rather than R because the callers decode into a local and
// pass its address; copying a reply struct per token lookup would be the only
// alternative.
//
// Decision record: the "stats family" (urchin, mcsr, fortnite, codm,
// clashroyale, valorant, external.go's generic RPC replies) stays on
// TokenExpander/StringPalette rather than moving onto Palette/Spec like every
// other reply surface. Migrating it would mean rewriting ~15 token maps
// across 8 files into Spec declarations for no behaviour change — the values
// already come off a decoded reply exactly the way Palette.Bind would want,
// so the only real win left is the shared pure fallback, which the one-line
// change below gives it directly. What DOES move: the miss path (this file)
// now answers the pure family instead of only {random}/{choice}, and
// reply_tokens.go derives its inventory from a Spec each stats module
// registers beside its TokenExpander (names+docs only, no second value path).
type TokenExpander[R any] map[string]func(*R) string

// Expand renders tmpl over r: a {token} this palette knows resolves from the
// reply, anything else falls through to the pure family (see pureFallback)
// and is left literal when even that does not know it.
func (t TokenExpander[R]) Expand(text string, r *R) string {
	return ExpandString(text, func(tok tmpl.Token) (string, bool) {
		if field, ok := t[tok.Key()]; ok {
			return field(r), true
		}
		return pureFallback(tok)
	})
}

// Names lists the palette's token names. It backs the Spec a stats module
// registers for ReplyTokenInventory() (reply_tokens.go): the names the
// runtime map already carries, without a second hand-maintained list to drift
// from it. Order is map order (random); callers that register a Spec from it
// sort before use, same as every other namespace.
func (t TokenExpander[R]) Names() []string {
	out := make([]string, 0, len(t))
	for name := range t {
		out = append(out, name)
	}
	return out
}

// StringPalette is a token palette whose values are already rendered strings.
//
// It is the non-generic sibling of TokenExpander, for replies whose tokens
// cannot be read off the reply alone: mcsr renders {elo} as the word
// "unrated" and {result} as a translated win/loss/draw, so its palettes are
// built per call against the channel's locale and merged out of fragments two
// commands share. Pushing those through TokenExpander[R] would mean inventing
// a view type per command whose only job is to carry a locale next to the
// reply.
type StringPalette map[string]string

// Expand renders tmpl over the palette: a {token} it holds resolves to that
// value, anything else falls through to the pure family (see pureFallback)
// and is left literal when even that does not know it. This is the same
// resolution order TokenExpander uses, so a broadcaster's template behaves
// identically whichever palette answers it.
func (p StringPalette) Expand(text string) string {
	return ExpandString(text, func(tok tmpl.Token) (string, bool) {
		if val, ok := p[tok.Key()]; ok {
			return val, true
		}
		return pureFallback(tok)
	})
}

// Merge folds fragment palettes into p, later entries winning, and returns the
// combined palette. p itself is not modified: a fragment shared by two
// commands is usually built by a helper both call, and merging in place would
// let one command's extra tokens leak into the other's.
func (p StringPalette) Merge(parts ...StringPalette) StringPalette {
	out := make(StringPalette, len(p)+len(parts)*4)
	for key, val := range p {
		out[key] = val
	}
	for _, part := range parts {
		for key, val := range part {
			out[key] = val
		}
	}
	return out
}

// SpecEntry is one token a module declares it can fill into a reply
// template: its name plus a one-line doc. A module writes these once, ahead
// of any particular call — no reply instance, no reply text, just the name
// and why it exists — which is what lets ReplyTokenInventory()
// (modules/reply_tokens.go) be DERIVED from the specs every module registers
// instead of hand-copied into a second literal that can drift from the code
// that actually fills the tokens in.
//
// Doc's job is documentation, not runtime behaviour: Resolve reads Name and
// Value off an Entry, never Doc. No module reads it back at runtime either —
// not even timeofday.go's timeReplySpec, whose Bind closure switches on name
// the same way every other Spec.Bind caller does. Doc's one consumer is
// ReplyTokenInventory() (reply_tokens.go), which copies it into
// reply_tokens.golden.json — the fixture web/kit/lib/catalog/*.ts's
// replyTokens() hint text is checked against — so a reviewer or a kit author
// can see what a token means without reading the Go handler that fills it
// in. That write-only path is deliberate, not an unfinished migration: a
// Spec entry with an empty Doc (the stats family's namesOnly entries,
// reply_tokens.go) is a documented choice — see that file's decision record
// — not a placeholder waiting to be filled in.
type SpecEntry struct {
	Name string
	Doc  string
}

// Spec is a module's static declaration of the reply tokens one reply can
// fill in: names and docs, no values. Bind attaches the per-call value
// functions to produce a Palette ready to resolve a render.
type Spec struct {
	Entries []SpecEntry
}

// Names lists the spec's declared token names, in declaration order.
func (s Spec) Names() []string {
	out := make([]string, len(s.Entries))
	for i, e := range s.Entries {
		out[i] = e.Name
	}
	return out
}

// Bind attaches a value to each declared entry, producing a Palette. value is
// called once per entry, at bind time — a reply is decoded once per call, so
// nothing is gained by delaying the lookup to first read — and must return a
// value func for every name the Spec declares (a nil func would panic on
// Resolve, which is the point: a Spec entry with no bound value is a bug in
// the module, not a broadcaster-visible literal).
func (s Spec) Bind(value func(name string) func() string) Palette {
	entries := make([]Entry, len(s.Entries))
	for i, e := range s.Entries {
		entries[i] = Entry{Name: e.Name, Doc: e.Doc, Value: value(e.Name)}
	}
	return Palette{entries: entries}
}

// Entry is one bound token: a name, its doc, and the closure that renders its
// value for the reply being expanded right now.
type Entry struct {
	Name  string
	Doc   string
	Value func() string
}

// Palette is the ordered, bound set of Entries one reply template resolves
// against. It is the one abstraction every module reply now expands through
// (see the package doc on Expand for the closure-based primitive it sits on
// top of): {tokens} the module declared resolve to their bound Value, and
// anything else falls through to the pure family — {random}, {choice},
// {math}, {countdown}, {countup}, {repeat}, {queryescape}, {pathescape} — the
// same grammar a custom command's template already has. `|fallback` and
// {if} keep working unchanged: they live in pkg/tmpl.appendSpan, upstream of
// Resolve, and never see a Palette at all.
type Palette struct {
	entries []Entry
	locale  string
}

// Names lists the palette's bound token names, in declaration order.
func (p Palette) Names() []string {
	out := make([]string, len(p.entries))
	for i, e := range p.entries {
		out[i] = e.Name
	}
	return out
}

// Merge folds other palettes into p, later entries winning by name — the
// same later-wins rule StringPalette.Merge used. p is not modified: a
// fragment two callers share (Common, in particular) must not pick up one
// caller's extra tokens through the other. The palette's locale follows the
// same rule: the last one that set it (non-empty) wins.
func (p Palette) Merge(others ...Palette) Palette {
	order := make([]string, 0, len(p.entries))
	byName := make(map[string]Entry, len(p.entries))
	add := func(e Entry) {
		if _, ok := byName[e.Name]; !ok {
			order = append(order, e.Name)
		}
		byName[e.Name] = e
	}
	for _, e := range p.entries {
		add(e)
	}
	locale := p.locale
	for _, other := range others {
		for _, e := range other.entries {
			add(e)
		}
		if other.locale != "" {
			locale = other.locale
		}
	}
	out := make([]Entry, len(order))
	for i, name := range order {
		out[i] = byName[name]
	}
	return Palette{entries: out, locale: locale}
}

// Resolve answers one lexed token against p: a name p declares resolves to
// that entry's Value, PROVIDED the span carries no payload — {user:x} on a
// name that means a bare value is half a token, the same rule the message
// scope's own tokens follow (engine/scope/message.go), and stays literal
// rather than silently ignoring the payload. Anything else falls through to
// the pure family (engine/scope/pure.go), evaluated fresh at render time so
// two {random} spans in one template still print two different numbers.
func (p Palette) Resolve(tok tmpl.Token) (string, bool) {
	for _, e := range p.entries {
		if e.Name == tok.Name {
			if tok.HasPayload {
				return "", false
			}
			return e.Value(), true
		}
	}
	// Now is left at its zero value (nil) on purpose: Pure.Now nil means
	// time.Now (engine/scope/clock.go), the real wall clock every reply
	// should render {countdown}/{countup} against. Only a test pins it, and
	// it does that on a scope.Pure it builds directly, not through Palette.
	values, _ := scope.Pure{Locale: p.locale}.Plan(nil, nil)
	return values.Get(tok)
}

// Expand renders tmpl over p, appending into dst (see the package Expand's
// doc for the pooled-buffer contract).
func (p Palette) Expand(dst []byte, s string) []byte {
	return tmpl.Append(dst, s, p.Resolve)
}

// ExpandString wraps Expand for callers who do not pool their own buffers.
func (p Palette) ExpandString(s string) string {
	return tmpl.Expand(s, p.Resolve)
}

// WithLocale sets the locale Resolve's pure-family fallback words
// {countdown}/{countup} with (engine/scope's humanizer, wired through
// i18n). A caller built from Common already carries the channel's locale
// (Common reads c.Locale), so Merge propagates it automatically; a caller
// that starts from KV(…) or a bare Spec.Bind(…) does not, and without this
// {countdown} would word itself in the catalog's English fallback on every
// non-English channel. It replaces the palette's locale outright rather
// than merging: a caller always knows its own channel's locale outright,
// there is nothing to reconcile.
func (p Palette) WithLocale(locale string) Palette {
	p.locale = locale
	return p
}

// KV adapts an even-length (name, value, name, value, …) list into a
// Palette. It is the one adapter every kv-style caller of chatReplier.reply
// (146 of them, across loyalty/duel/raffle/queue/songqueue/quotes/gamble)
// keeps working through unchanged: they still pass token/value pairs, and
// chatReplier folds the pairs into a Palette once, in one place, instead of
// each caller building its own.
//
// A repeated name resolves to its LAST pair, not its first: raffle_mechanics'
// pre-Palette expandTokens built a map[string]string from the same kv shape,
// where a later key overwrites an earlier one, and Merge's later-wins rule
// makes the same promise for two Palettes. KV("a", "1", "a", "2") and
// KV("a", "1").Merge(KV("a", "2")) must resolve {a} the same way, so the
// dedupe happens here, once, rather than leaving Resolve's per-token scan to
// find the first match and silently disagree with Merge. An empty name is
// dropped outright: no lexed Token ever has one, so a pair shaped that way
// could never resolve and would only cost Resolve a wasted comparison.
//
// The result is still a linear list, not a map: no caller passes more than a
// handful of pairs, and Resolve's own scan stays a straight loop over
// Entries — this is the same trade-off the pre-Palette chatReplier.reply
// already made explicit.
func KV(kv ...string) Palette {
	order := make([]string, 0, len(kv)/2)
	byName := make(map[string]string, len(kv)/2)
	for i := 0; i+1 < len(kv); i += 2 {
		name, val := kv[i], kv[i+1]
		if name == "" {
			continue
		}
		if _, seen := byName[name]; !seen {
			order = append(order, name)
		}
		byName[name] = val
	}
	entries := make([]Entry, len(order))
	for i, name := range order {
		val := byName[name]
		entries[i] = Entry{Name: name, Value: func() string { return val }}
	}
	return Palette{entries: entries}
}

// Common is the {user}/{channel} pair every module reply used to bind by
// hand: {user} the invoking chatter's login, {channel} the broadcaster's
// login. It is defined once and merged first — a caller's own Palette merges
// in after it and wins on any name it also declares, which is how the
// handful of replies that want {user} to mean the chatter's DISPLAY name
// (personality.go, emoteplay.go, channelpoints.go's redeemer) keep that
// meaning: they simply declare their own {user} entry, and Merge's
// later-wins rule lets it shadow Common's.
//
// {points}, the wager games' currency word, is deliberately NOT here: it has
// no value without a channel's loyalty config, which Common (c *Context
// alone, no ctx, no engine.Deps) cannot read. It stays where the games
// already compute it, bound through KV alongside their other tokens — same
// as it worked before this package existed.
func Common(c *Context) Palette {
	return Palette{
		entries: []Entry{
			{Name: "user", Doc: "the invoking chatter's login", Value: func() string { return c.Env.ChatterUserLogin }},
			{Name: "channel", Doc: "the broadcaster's login", Value: func() string { return c.Env.BroadcasterUserLogin }},
		},
		locale: c.Locale,
	}
}
