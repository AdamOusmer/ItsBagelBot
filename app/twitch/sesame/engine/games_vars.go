// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"

	"ItsBagelBot/app/twitch/sesame/engine/scope"
	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/pkg/codec"
	"ItsBagelBot/pkg/tmpl"

	"go.uber.org/zap"
)

// GameMount is everything one game-stat token family needs to bind itself to a
// single command run.
//
// It exists because the palettes cannot live here. Every one of them is written
// against a gossip reply type in the modules package, which imports this one;
// re-deriving them on this side would be a second formatter per game, free to
// print a K/D to two decimals where the command that owns it prints three. So
// the modules contribute their families as values, and this is the handshake:
// the engine says which broadcaster is asking and hands over their decoded
// module row, the module says what the tokens are worth.
type GameMount struct {
	// Ctx is the chat context of the command being expanded: the channel's
	// locale (a palette may translate a word), the broadcaster's own login (the
	// last step of the linked-account fallback) and the premium lane.
	Ctx *module.Context
	// Config is this broadcaster's module config blob, exactly as the module's
	// own commands decode it: the linked account, the "only my account" toggle
	// and whatever else scopes a lookup live in it.
	Config codec.RawMessage
	// Gossip is the caller the module's own commands use, so a token rides the
	// same cache and never opens a second one.
	Gossip GossipCaller
	// Log carries the family's own failures; a failed lookup renders empty
	// either way (see scope.GameLookup).
	Log *zap.Logger
}

// GameFamilySpec is one prefixed game-stat token family as its module declares
// it: how the tokens are spelled, which per-broadcaster module row switches them
// on, which fields exist, and how to bind the lookup to one run.
//
// Prefix and Fields are static so the engine can tell whether a template names
// this family before it reads a single module row: a custom command mentioning
// no game token — the overwhelming majority — costs no projection read at all.
type GameFamilySpec struct {
	Prefix string
	// Module is the opt-in module row that gates the family, the very row the
	// module's own commands check. A token reading it under its own polarity
	// would leave one channel where the command runs and the token does not.
	Module string
	Fields []string
	Lookup func(GameMount) scope.GameLookup
}

// owns reports whether a token name falls in this family, by the same rule the
// scope answers it with.
func (s GameFamilySpec) owns(name string) bool {
	_, ok := s.recognizer().Field(name)
	return ok
}

// recognizer is the static half of the spec as the scope's own type: the
// spelling and the field list, with no lookup bound. It exists so the
// before-any-module-row check above answers by exactly the code that answers
// after mounting, rather than by a second copy of the prefix rule.
func (s GameFamilySpec) recognizer() scope.GameFamily {
	return scope.GameFamily{Prefix: s.Prefix, Fields: s.Fields}
}

// gamesScope builds the game-stat scope for one command run, mounted per
// family: each is gated by its own opt-in module, so a channel with Valorant on
// and Fortnite off expands {val.tier} and leaves {fn.kd} visible.
func (p *Pipeline) gamesScope(ctx context.Context, c *module.Context, toks []tmpl.Token) (scope.Games, bool) {
	if p.gossip == nil {
		return scope.Games{}, false
	}
	wanted := gameSpecsOf(p.games, toks)
	if len(wanted) == 0 {
		return scope.Games{}, false
	}
	rows := gameRows{p: p, c: c, on: make(map[string]bool, len(wanted))}
	families := make([]scope.GameFamily, 0, len(wanted))
	for _, spec := range wanted {
		families = p.mountFamily(ctx, &rows, families, spec)
	}
	return scope.Games{Families: families}, len(families) > 0
}

// mountFamily adds one family when its module is on for this broadcaster.
func (p *Pipeline) mountFamily(ctx context.Context, rows *gameRows, into []scope.GameFamily, spec GameFamilySpec) []scope.GameFamily {
	config, on := rows.view(ctx, spec.Module)
	if !on {
		return into
	}
	mount := GameMount{Ctx: rows.c, Config: config, Gossip: p.gossip, Log: p.log}
	family := spec.recognizer()
	family.Lookup = spec.Lookup(mount)
	return append(into, family)
}

// gameSpecsOf keeps the families this template actually names.
func gameSpecsOf(specs []GameFamilySpec, toks []tmpl.Token) []GameFamilySpec {
	wanted := make([]GameFamilySpec, 0, len(specs))
	for _, spec := range specs {
		if namesGameFamily(spec, toks) {
			wanted = append(wanted, spec)
		}
	}
	return wanted
}

func namesGameFamily(spec GameFamilySpec, toks []tmpl.Token) bool {
	for _, tok := range toks {
		if tok.Kind == tmpl.KindVar && spec.owns(tok.Name) {
			return true
		}
	}
	return false
}

// gameRows reads each module row once per run and remembers the answer.
//
// A module can contribute more than one family — Clash Royale's lifetime,
// ranked and trophy-road views are three prefixes over one dashboard page — and
// reading its row once per family would spend three projection reads on one
// switch, with the added risk that two of them disagree mid-response.
type gameRows struct {
	p       *Pipeline
	c       *module.Context
	on      map[string]bool
	configs map[string]codec.RawMessage
}

func (r *gameRows) view(ctx context.Context, name string) (codec.RawMessage, bool) {
	if on, done := r.on[name]; done {
		return r.configs[name], on
	}
	view, on := r.p.moduleGate(r.c, name).OptInView(ctx)
	r.on[name] = on
	if r.configs == nil {
		r.configs = make(map[string]codec.RawMessage, len(r.on))
	}
	r.configs[name] = view.Configs
	return view.Configs, on
}
