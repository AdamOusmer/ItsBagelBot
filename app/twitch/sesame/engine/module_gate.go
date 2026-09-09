// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"

	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/internal/projection"

	"go.uber.org/zap"
)

// ModuleState is the outcome of resolving one broadcaster's module row.
//
// Three states, not a bool, because the four consumers of a module row disagree
// about what "could not read it" means and that disagreement is deliberate:
// the built-in commands (!followage, !clip) fail OPEN, since a projection blip
// must not silently swallow a viewer's command, while the background machinery
// (loyalty rates, the timer clock) fails CLOSED, since paying out points or
// posting timers off a config we could not read is worse than doing nothing.
// A two-state gate would have had to pick one polarity and lie to the other
// half of the callers; keeping "unavailable" its own state lets each caller
// spend exactly one branch mapping it onto its own policy.
type ModuleState uint8

const (
	// ModuleUnavailable means the row could not be resolved: the read failed,
	// or no projection is wired at all. It carries no view.
	ModuleUnavailable ModuleState = iota
	// ModuleOff means the row resolved and the module is not to run.
	ModuleOff
	// ModuleOn means the row resolved and the module is to run. Only this
	// state guarantees a usable view (and therefore a usable Configs blob).
	ModuleOn
)

// ModuleLookup names the row to resolve and the policy for resolving it.
//
// A struct rather than four parameters: the call takes a uint64, a string and
// a ModuleState, and every ordering of those compiles. Transposing the id and
// the name is the kind of swap that reads fine and gates the wrong module, so
// the fields are named at the call site. (This shape is also what the
// code-health gate asks for over a five-argument function.)
type ModuleLookup struct {
	// Proj may be nil: a build with no projection wired resolves every row as
	// ModuleUnavailable rather than panicking.
	Proj          projection.Reader
	BroadcasterID uint64
	Name          string
	// Absent is what a MISSING row means to this caller, the one piece of the
	// preamble that is not shared: a built-in command ships enabled, so no row
	// means ModuleOn, while a dashboard-configured module has nothing to run
	// without its row, so no row means ModuleOff.
	Absent ModuleState
}

// Resolve turns the lookup into the decision every module consumer needs: run
// or don't, plus the view carrying the config blob.
//
// It exists because four call sites had grown the same four-clause preamble
// (read error, row missing, row disabled, blob empty) inlined into one
// conditional each; that shape is what the code-health gate flags, and patching
// it per call site just moves the finding to the next one. The preamble lives
// here once and each caller keeps only the part that is genuinely its own: its
// fail-open/fail-closed policy and what it does with the blob.
//
// err is returned rather than logged here: the callers log with their own
// prefix and fields, and the loyalty path deliberately logs nothing at all.
func (l ModuleLookup) Resolve(ctx context.Context) (projection.ModuleView, ModuleState, error) {
	if l.Proj == nil {
		return projection.ModuleView{}, ModuleUnavailable, nil
	}
	// Keyed read: Module indexes the cached by-name map, so this costs a map
	// lookup instead of a walk over every module the broadcaster has.
	view, ok, err := l.Proj.Module(ctx, l.BroadcasterID, l.Name)
	if err != nil {
		return projection.ModuleView{}, ModuleUnavailable, err
	}
	if !ok {
		return projection.ModuleView{}, l.Absent, nil
	}
	if !view.IsEnabled {
		return view, ModuleOff, nil
	}
	return view, ModuleOn, nil
}

// ModuleGate is one module's per-broadcaster row, named
// the way ModuleLookup beside it is: everything the read needs on one value,
// so a caller cannot hand the projection of one broadcaster the id of another.
//
// It lives here rather than in the modules package because the {followage} and
// {accountage} response tokens are gated by exactly the same rows as
// !followage and !accountage, and a token that read those rows under its own
// polarity would leave one channel where the command runs and the token does
// not. Log may be nil.
type ModuleGate struct {
	Proj          projection.Reader
	Log           *zap.Logger
	BroadcasterID uint64
	Name          string
}

// BuiltinEnabled reports whether a BUILT-IN command's toggle is on. A missing
// row (or a projection read error, or no projection at all) fails open: a
// transient blip must not silently swallow the command.
func (g ModuleGate) BuiltinEnabled(ctx context.Context) bool {
	// absent is ModuleOn because the built-ins ship enabled: a broadcaster who
	// never touched the toggle has no row, and that must not read as "off".
	_, state, err := ModuleLookup{Proj: g.Proj, BroadcasterID: g.BroadcasterID, Name: g.Name, Absent: ModuleOn}.Resolve(ctx)
	if err != nil && g.Log != nil {
		g.Log.Warn(g.Name+": module state read failed, allowing", module.BIDField(g.BroadcasterID), zap.Error(err))
	}
	// Fail open: only an explicit off suppresses it. ModuleUnavailable (read
	// error, or no projection wired at all) runs, which is why this compares
	// against ModuleOff rather than for ModuleOn.
	return state != ModuleOff
}

// OptInView resolves a dashboard-configured (opt-in) module's row and
// reports whether it is on for this broadcaster, handing back the view so the
// caller can decode its config blob.
//
// It is the opposite polarity to BuiltinEnabled and the difference is the
// whole point of having both: a built-in ships enabled, so a missing row is ON,
// while an opt-in module has nothing to run without its row, so a missing row —
// and an unreadable one — is OFF. The {quote}, {time} and {song} response
// tokens are gated by exactly the same rows as !quote, !time and !song, and a
// token reading those rows under its own polarity would leave one channel where
// the command runs and the token does not. log may be nil.
func (g ModuleGate) OptInView(ctx context.Context) (projection.ModuleView, bool) {
	view, state, err := ModuleLookup{Proj: g.Proj, BroadcasterID: g.BroadcasterID, Name: g.Name, Absent: ModuleOff}.Resolve(ctx)
	if err != nil && g.Log != nil {
		g.Log.Warn(g.Name+": module state read failed, token stays literal", module.BIDField(g.BroadcasterID), zap.Error(err))
	}
	return view, state == ModuleOn
}
