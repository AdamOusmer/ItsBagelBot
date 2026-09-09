// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package scope

import (
	"context"

	"ItsBagelBot/app/twitch/sesame/module"
)

// Pure answers the tokens that depend on nothing outside the span itself:
// {random}, {random:min-max} and {choice:a,b,c}. It is always mounted — there
// is no dependency to be missing — and is first in the chain so a later scope
// can never shadow the dice.
type Pure struct{}

// Owns claims the two generic dynamic names module.ParseDynamic answers.
func (Pure) Owns(name string) bool {
	return name == "random" || name == "choice"
}

// Plan does no work: a pure token needs no ctx, no batching and no lookup.
func (Pure) Plan(context.Context, []Var) (Values, error) {
	return pureValues{}, nil
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
type pureValues struct{}

func (pureValues) Get(v Var) (string, bool) {
	return module.ParseDynamic(v.Key())
}
