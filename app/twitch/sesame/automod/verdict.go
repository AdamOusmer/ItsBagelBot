// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package automod

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

type Verdict struct {
	Action  Action
	Seconds uint32
	Rule    string
}
