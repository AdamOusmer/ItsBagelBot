// Package automod is sesame's inline chat guard. It runs before command dispatch
// on every chat line and returns a Verdict; in shadow mode the pipeline logs the
// verdict and takes no action. Tier 0 (trust) and Tier 1 (content) live here; the
// centralized valkey signals, the trained classifier and the mod queue arrive in
// later phases (see docs/automod/PLAN.md and IMPLEMENTATION.md).
package automod

// Action is the moderation action a verdict calls for, ordered by severity so a
// caller can compare and pick the strongest.
type Action uint8

const (
	ActionNone Action = iota
	ActionWarn
	ActionDelete
	ActionRestrict
	ActionTimeout
	ActionBan
)

func (a Action) String() string {
	switch a {
	case ActionWarn:
		return "warn"
	case ActionDelete:
		return "delete"
	case ActionRestrict:
		return "restrict"
	case ActionTimeout:
		return "timeout"
	case ActionBan:
		return "ban"
	default:
		return "none"
	}
}

// Verdict is the gate's decision for one message. It is a value type so Inspect
// returns it on the stack with no heap allocation regardless of outcome.
type Verdict struct {
	Action  Action
	Seconds uint32 // timeout length in seconds; 0 for non-timeout actions
	Rule    string // which signal fired, for shadow logging and audit
}
