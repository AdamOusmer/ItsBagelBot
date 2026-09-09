// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package scope

import (
	"context"
	"strings"
)

// counterName is the token this scope answers: {counter:deaths} bumps the
// broadcaster's "deaths" counter by one and renders the new value.
const counterName = "counter"

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
// drop one leading '!', trim again, lower-case. It lives here rather than in
// the engine because the token grammar has to fold a payload BEFORE the store
// is reached (so "{COUNTER:Deaths}" and "{counter:deaths}" are one bump, not
// two), and engine.NormalizeCounterName delegates to it so the two can never
// answer differently.
func NormalizeName(name string) string {
	return strings.ToLower(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(name), "!")))
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

// Store answers {counter:...} by bumping the named counter once per run.
//
// It is mounted only when a loyalty store is wired; without one, every counter
// token stays literal rather than rendering a misleading zero.
type Store struct {
	Counters Counters
}

// Owns claims the counter token family.
func (Store) Owns(name string) bool { return name == counterName }

// Plan bumps each distinct counter the template names, once, in first
// appearance order — the order the old scanner produced and the order the
// tests assert, so a response naming two counters still records them in the
// sequence the broadcaster wrote them.
func (s Store) Plan(ctx context.Context, wants []Var) (Values, error) {
	out := make(counterValues, len(wants))
	done := make(map[string]struct{}, len(wants))
	for _, v := range wants {
		ref, ok := counterRefOf(v)
		if !ok {
			continue
		}
		if _, dup := done[ref.key]; dup {
			continue // two spellings of one counter: one bump, both render it
		}
		done[ref.key] = struct{}{}
		s.bumpInto(ctx, out, ref)
	}
	return out, nil
}

// bumpInto records one counter's new value, leaving an unresolvable counter
// out of the map so its token stays visible.
func (s Store) bumpInto(ctx context.Context, out counterValues, ref counterRef) {
	if value := s.Counters.Bump(ctx, ref.name, ref.addressed); value != "" {
		out[ref.key] = value
	}
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

// counterRefOf parses a {counter:...} payload, reporting false for every
// spelling that must stay literal: no payload at all ({counter}), an empty
// name, an addressing prefix with nothing after it ({counter:target:}), and
// any bot-scope counter.
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

// counterValues is one run's bumped values, keyed by the folded payload so
// every spelling of a counter reads the value its single bump produced.
type counterValues map[string]string

func (m counterValues) Get(v Var) (string, bool) {
	ref, ok := counterRefOf(v)
	if !ok {
		return "", false
	}
	value, ok := m[ref.key]
	return value, ok
}
