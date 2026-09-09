// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package scope

import (
	"context"
	"strings"

	"ItsBagelBot/pkg/tmpl"
)

// The two tokens this scope answers. {counter:deaths} bumps the broadcaster's
// "deaths" counter by one and renders the new value; {count:deaths} reads the
// same counter and renders it without touching it.
//
// Two names rather than one name plus a modifier ({counter:deaths:peek}) is a
// pinned decision: the modifier form makes a typo — a dropped suffix, a
// mis-spelled one — silently promote a read into a write, and a write to a
// counter is the one effect in this palette that a broadcaster cannot see and
// cannot undo from chat. Two names cost one extra word in the docs and make
// the mistake impossible to spell.
const (
	counterName = "counter"
	countName   = "count"
)

// botCounterPrefix marks a bot-scope counter reference inside a counter token
// ({counter:bot:feeds}). Bot counters are admin-only: broadcaster commands
// never resolve or bump them, so the token is skipped and stays visible,
// exactly like any other unknown token. Only admin/system-authored content may
// resolve it.
const botCounterPrefix = "bot:"

// targetCounterPrefix marks a target-addressed counter reference inside a
// counter token ({counter:target:shutups}): the bump keys on the viewer the
// command mentions ({touser}) instead of the sender, so "!shutup @bob" counts
// against bob. The counter's own scope still decides the bucket shape — the
// addressing only changes whose viewer identity rides the bump (issue #479).
// Like "bot:", the "target:" spelling inside a counter name is reserved by the
// worker's token grammar.
const targetCounterPrefix = "target:"

// NormalizeName folds a counter name the way the loyalty store does: trim,
// drop one leading '!', trim again, lower-case.
//
// The fold itself moved to tmpl.NormalizeName, because it is part of the
// token grammar and the command repository (app/db) has to apply the same one
// to answer "which commands reference this urlfetch definition" — and app/db
// may not import sesame. This name stays as the spelling every scope and
// engine.NormalizeCounterName already reads, delegating so the three can
// never answer differently.
func NormalizeName(name string) string {
	return tmpl.NormalizeName(name)
}

// Counters is the bump the store scope delegates to. The engine implements it
// over the loyalty store, carrying the run's identity (sender, mentioned
// viewer, command name) and the redelivery guard — none of which is grammar,
// which is why none of it is in this package.
//
// An empty result means "no value": the bump failed, or the counter is not one
// this caller may read. The token then stays visible, matching every other
// unresolved token.
type Counters interface {
	Bump(ctx context.Context, name string, addressed bool) (value string)
}

// Peeks is the read {count:...} delegates to — the same counter identified the
// same way, with no write of any kind behind it.
//
// It is a separate interface from Counters rather than a second method on it so
// that "this dependency can only read" is a fact about the type the engine
// hands over, not a promise in a doc comment. An empty result means the counter
// does not exist (nothing has ever bumped it) or could not be read.
type Peeks interface {
	Peek(ctx context.Context, name string, addressed bool) (value string)
}

// Store answers the counter token family: {counter:...} bumps and renders,
// {count:...} only reads.
//
// It is mounted only when a loyalty store is wired; without one, every counter
// token stays literal rather than rendering a misleading zero.
type Store struct {
	Counters Counters
	Peeks    Peeks
}

// Owns claims a token only when the dependency that answers it is mounted, so
// a build wired for reads alone leaves {counter:...} literal rather than
// silently answering it without bumping.
func (s Store) Owns(name string) bool {
	switch name {
	case counterName:
		return s.Counters != nil
	case countName:
		return s.Peeks != nil
	}
	return false
}

// Plan bumps each distinct counter the template names, once, in first
// appearance order — the order the old scanner produced and the order the
// tests assert, so a response naming two counters still records them in the
// sequence the broadcaster wrote them — and then reads the ones only named
// read-only.
//
// Bumps run BEFORE peeks, in a pass of their own. That ordering is a pinned
// decision, and it is what makes a template carrying both spellings of one
// counter ("that is {counter:deaths} deaths, {count:deaths} today") coherent:
// {count:deaths} reports the value the bump beside it produced, not the value
// from before it, and it costs no read at all because the bumped value is
// reused. The alternative — resolving each span in written order — would make
// the same two spans print two different numbers depending on which the
// broadcaster typed first, which reads as a bug in the bot every time.
func (s Store) Plan(ctx context.Context, wants []Var) (Values, error) {
	out := &storeValues{
		bumped: make(map[string]string, len(wants)),
		peeked: make(map[string]string, len(wants)),
	}
	for _, v := range wants {
		s.bumpInto(ctx, out, v)
	}
	for _, v := range wants {
		s.peekInto(ctx, out, v)
	}
	return out, nil
}

// bumpInto records one {counter:...} span's new value, leaving an unresolvable
// counter out of the map so its token stays visible. A counter already bumped
// by an earlier spelling is not bumped again.
func (s Store) bumpInto(ctx context.Context, out *storeValues, v Var) {
	ref, ok := refFor(v, counterName)
	if !ok {
		return
	}
	if _, dup := out.bumped[ref.key]; dup {
		return // two spellings of one counter: one bump, both render it
	}
	if value := s.Counters.Bump(ctx, ref.name, ref.addressed); value != "" {
		out.bumped[ref.key] = value
	}
}

// peekInto records one {count:...} span's value, reusing this run's bump when
// the template also bumped that counter.
//
// A counter nobody has ever bumped records the EMPTY string rather than being
// left out: the read ran and honestly answered "nothing", so the span renders
// its fallback ({count:deaths|none yet}) instead of staying literal, which
// would claim the bot has no such token.
func (s Store) peekInto(ctx context.Context, out *storeValues, v Var) {
	ref, ok := refFor(v, countName)
	if !ok {
		return
	}
	if _, done := out.peeked[ref.key]; done {
		return
	}
	if bumped, ok := out.bumped[ref.key]; ok {
		out.peeked[ref.key] = bumped
		return
	}
	out.peeked[ref.key] = s.Peeks.Peek(ctx, ref.name, ref.addressed)
}

// refFor parses a span that belongs to name, reporting false for every other
// span so each pass over wants handles only its own family.
func refFor(v Var, name string) (counterRef, bool) {
	if v.Name != name {
		return counterRef{}, false
	}
	return counterRefOf(v)
}

// counterRef is one counter token's parsed payload: the folded key two
// spellings of the same counter share, the bucket name the store is asked for
// (the addressing prefix removed), and whether the bump keys on the mentioned
// viewer.
type counterRef struct {
	key       string
	name      string
	addressed bool
}

// counterRefOf parses a {counter:...} or {count:...} payload — one grammar for
// both spellings, so a counter is addressed identically whether it is being
// bumped or read — reporting false for every spelling that must stay literal:
// no payload at all ({counter}, {count}), an empty name, an addressing prefix
// with nothing after it ({count:target:}), and any bot-scope counter.
//
// The bucket name is NOT re-folded after the "target:" prefix comes off — the
// loyalty store folds what it is given, and folding twice here would make
// "{counter:target: deaths}" bump a different bucket than it does today.
func counterRefOf(v Var) (counterRef, bool) {
	if !v.HasPayload {
		return counterRef{}, false
	}
	key := NormalizeName(v.Payload)
	base, addressed := strings.CutPrefix(key, targetCounterPrefix)
	if base == "" || strings.HasPrefix(base, botCounterPrefix) {
		return counterRef{}, false
	}
	return counterRef{key: key, name: base, addressed: addressed}, true
}

// storeValues is one run's counter answers, keyed by the folded payload so
// every spelling of a counter reads the value its single lookup produced.
//
// The two maps are separate because their MISSING entries mean opposite
// things: a counter absent from bumped could not be bumped and stays literal,
// while a counter present in peeked with an empty value was read and found to
// be nothing, which renders the span's fallback.
type storeValues struct {
	bumped map[string]string
	peeked map[string]string
}

func (m *storeValues) Get(v Var) (string, bool) {
	ref, ok := counterRefOf(v)
	if !ok {
		return "", false
	}
	if v.Name == countName {
		value, ok := m.peeked[ref.key]
		return value, ok
	}
	value, ok := m.bumped[ref.key]
	return value, ok
}
