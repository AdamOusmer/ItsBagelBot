// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"

	"ItsBagelBot/internal/projection"
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

// ModuleGate resolves one broadcaster's module row into the decision every
// module consumer needs: run or don't, plus the view carrying the config blob.
//
// It exists because four call sites had grown the same four-clause preamble
// (read error, row missing, row disabled, blob empty) inlined into one
// conditional each; that shape is what the code-health gate flags, and patching
// it per call site just moves the finding to the next one. The preamble lives
// here once and each caller keeps only the part that is genuinely its own: its
// fail-open/fail-closed policy and what it does with the blob.
//
// absent is what a MISSING row means to this caller, which is the one piece of
// the preamble that is not shared: a built-in command ships enabled, so no row
// means ModuleOn, while a dashboard-configured module has nothing to run
// without its row, so no row means ModuleOff.
//
// err is returned rather than logged here: the callers log with their own
// prefix and fields, and the loyalty path deliberately logs nothing at all.
func ModuleGate(ctx context.Context, proj projection.Reader, broadcasterID uint64, name string, absent ModuleState) (projection.ModuleView, ModuleState, error) {
	if proj == nil {
		return projection.ModuleView{}, ModuleUnavailable, nil
	}
	// Keyed read: Module indexes the cached by-name map, so this costs a map
	// lookup instead of a walk over every module the broadcaster has.
	view, ok, err := proj.Module(ctx, broadcasterID, name)
	if err != nil {
		return projection.ModuleView{}, ModuleUnavailable, err
	}
	if !ok {
		return projection.ModuleView{}, absent, nil
	}
	if !view.IsEnabled {
		return view, ModuleOff, nil
	}
	return view, ModuleOn, nil
}
