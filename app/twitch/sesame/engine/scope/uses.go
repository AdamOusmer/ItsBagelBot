// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package scope

import (
	"context"
	"strconv"
)

// usesName is the one token this scope owns: {uses}, the lifetime execution
// count of the custom command that is running.
const usesName = "uses"

// Uses answers {uses} from the count the command row already carries.
//
// It is a scope of its own rather than a field on Message because the two
// answer different questions: Message is the chat line that triggered the run,
// this is the command row that matched it. Only runCustom builds a chain, so
// mounting it there is also what makes {uses} literal everywhere else — a
// built-in or a module reply never reaches this palette, and a broadcaster who
// types {uses} into one sees the token instead of a number that would be
// counting the wrong thing.
//
// Count is the value the commands service has already durably summed, which is
// why the token reads "excluding this run" (pinned): the tick for the run doing
// the rendering is published after the reply is emitted, and would not be in
// this number even if the pipeline wanted it to be. Rendering "including this
// run" would mean adding 1 here, and that +1 would be a lie for every run whose
// tick is later deduped as a redelivery or dropped at the reporter's key cap.
type Uses struct {
	Count uint64
}

// Owns claims {uses} and nothing else.
func (Uses) Owns(name string) bool { return name == usesName }

// Plan hands the struct back: the count arrived with the command row, so there
// is nothing to look up and no ctx to spend.
func (u Uses) Plan(context.Context, []Var) (Values, error) { return u, nil }

// Get renders the count.
//
// A never-used command renders "0" rather than the empty string, matching the
// pinned rule for {channel.viewers} offline: zero is the honest answer to
// "how many times", not a lookup that came back with nothing, so a fallback
// ({uses|never}) deliberately does not fire on it.
//
// A payload stays literal — {uses} is the token, {uses:hug} is not one — so
// the spelling stays free to mean "another command's count" later.
func (u Uses) Get(v Var) (string, bool) {
	if v.Name != usesName || v.HasPayload {
		return "", false
	}
	return strconv.FormatUint(u.Count, 10), true
}
