// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package scope

import (
	"context"
	"strings"

	"ItsBagelBot/pkg/tmpl"
)

// The two spellings this scope answers, both reads: {counter:deaths} and
// {count:deaths} render the same counter's current value. Neither writes.
//
// counter is canonical and count is its alias — kept as two names rather
// than folded into one because the command-run option that replaced the old
// write ("also bump counter <name> when this command runs", see
// ent/schema/commands.go's bump_counter field) is the only place a counter's
// value now changes, and a broadcaster who has been typing {counter:deaths}
// in a template for months should not have to relearn a spelling the day the
// bump moves out of it.
//
// {counter:x} used to BUMP and render the new value; {count:x} was its
// read-only twin. That write was the only side-effecting token in an
// otherwise pure-read chat template, and it sat one dropped letter from its
// own read-only twin — a typo (or a copy-paste of the wrong spelling into a
// second command) silently turned a read into an uncounted extra bump. An
// option a broadcaster picks from a list cannot be mistyped the way a token
// spelling can, so the bump moved there and both spellings settled on read.
const (
	counterName = "counter"
	countName   = "count"
)

// targetCounterPrefix marks a target-addressed counter reference inside a
// counter token ({counter:target:shutups}): the read keys on the viewer the
// command mentions ({touser}) instead of the sender, matching the addressing
// the command-option bump uses for the same counter (issue #479). The
// counter's own scope still decides the bucket shape — the addressing only
// changes whose viewer identity the read looks up.
const targetCounterPrefix = "target:"

// NormalizeName folds a counter name the way the loyalty store does: trim,
// drop one leading '!', trim again, lower-case.
//
// The fold itself moved to tmpl.NormalizeName, because it is part of the
// token grammar and the command repository (app/db) has to apply the same one
// to the bump_counter option at write time — and app/db may not import
// sesame. This name stays as the spelling every scope and
// engine.NormalizeCounterName already reads, delegating so none of them can
// answer differently.
func NormalizeName(name string) string {
	return tmpl.NormalizeName(name)
}

// Peeks is the read both {counter:...} and {count:...} delegate to: the
// named counter's current value, with no write of any kind behind it. An
// empty result means the counter does not exist (nothing has ever bumped it,
// whether through the old token or the current command-run option) or could
// not be read.
type Peeks interface {
	Peek(ctx context.Context, name string, addressed bool) (value string)
}

// Store answers the counter token family: {counter:...} and {count:...},
// both reads.
//
// It is mounted only when a loyalty store is wired; without one, every
// counter token stays literal rather than rendering a misleading zero.
type Store struct {
	Peeks Peeks
}

// Owns claims counter reads only when the dependency that answers them is
// mounted, so a build with no loyalty store leaves every counter token
// literal rather than silently answering it as empty.
//
// Bare {count} (no payload) is NOT claimed here — that spelling belongs to
// Uses, the {uses} alias — only {count:x} is. Owns takes the whole Var
// rather than just the name so this scope can tell the two apart instead of
// racing Uses for the name "count" and hoping chain order sorts it out; see
// the Scope.Owns doc for why a name-only Owns cannot make this split.
func (s Store) Owns(v Var) bool {
	if s.Peeks == nil {
		return false
	}
	switch v.Name {
	case counterName:
		return true
	case countName:
		return v.HasPayload
	}
	return false
}

// Plan reads each distinct counter the template names, once, regardless of
// which of the two spellings named it — "{counter:deaths} deaths, that is
// {count:deaths} today" costs one lookup, not two.
func (s Store) Plan(ctx context.Context, wants []Var) (Values, error) {
	out := &storeValues{peeked: make(map[string]string, len(wants))}
	for _, v := range wants {
		s.peekInto(ctx, out, v)
	}
	return out, nil
}

// peekInto records one counter span's value, keyed by its folded payload so
// both spellings of one counter share the single lookup's answer.
//
// A counter nobody has ever bumped records the EMPTY string rather than being
// left out: the read ran and honestly answered "nothing", so the span renders
// its fallback ({count:deaths|none yet}) instead of staying literal, which
// would claim the bot has no such token.
func (s Store) peekInto(ctx context.Context, out *storeValues, v Var) {
	ref, ok := counterRefOf(v)
	if !ok {
		return
	}
	if _, done := out.peeked[ref.key]; done {
		return
	}
	out.peeked[ref.key] = s.Peeks.Peek(ctx, ref.name, ref.addressed)
}

// counterRef is one counter token's parsed payload: the folded key both
// spellings of the same counter share, the bucket name the store is asked
// for (the addressing prefix removed), and whether the read keys on the
// mentioned viewer.
type counterRef struct {
	key       string
	name      string
	addressed bool
}

// counterRefOf parses a {counter:...} or {count:...} payload — one grammar
// for both spellings — reporting false for every spelling that must stay
// literal: no payload at all ({counter}, {count}), an empty name, and an
// addressing prefix with nothing after it ({count:target:}).
//
// The bucket name is NOT re-folded after the "target:" prefix comes off — the
// loyalty store folds what it is given, and folding twice here would make
// "{counter:target: deaths}" read a different bucket than the bump that fed
// it.
func counterRefOf(v Var) (counterRef, bool) {
	if !v.HasPayload {
		return counterRef{}, false
	}
	key := NormalizeName(v.Payload)
	base, addressed := strings.CutPrefix(key, targetCounterPrefix)
	if base == "" {
		return counterRef{}, false
	}
	return counterRef{key: key, name: base, addressed: addressed}, true
}

// storeValues is one run's counter reads, keyed by the folded payload so
// every spelling of a counter reads the value its single lookup produced.
type storeValues struct {
	peeked map[string]string
}

func (m *storeValues) Get(v Var) (string, bool) {
	ref, ok := counterRefOf(v)
	if !ok {
		return "", false
	}
	value, ok := m.peeked[ref.key]
	return value, ok
}
