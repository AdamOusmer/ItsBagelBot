// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package automation

import (
	"slices"

	"ItsBagelBot/pkg/codec"
)

type entry struct {
	check  func(c chk, raw codec.RawMessage)
	decode func(raw codec.RawMessage) (any, error)
	nested func(raw codec.RawMessage) [][]Action
	plan   func(params any) nesting
}

type nesting struct {
	lists     [][]Action
	labels    []string
	condition *Condition
}

var (
	triggerRegistry   map[string]entry
	conditionRegistry map[string]entry
	actionRegistry    map[string]entry
)

const roleModerator = 3

var requiredTrigger = map[string]string{
	"delete_message": "command",
	"redemption":     "reward",
}

var actionRole = map[string]int{
	"timeout": roleModerator, "ban": roleModerator, "warn": roleModerator, "shield": roleModerator,
	"delete_message": roleModerator,
}

func register[T any](check func(chk, T)) entry {
	return entry{
		check: func(c chk, raw codec.RawMessage) {
			p, err := DecodeParams[T](raw)
			if err != nil {
				c.add("", paramsCode(err))
				return
			}
			check(c, p)
		},
		decode: func(raw codec.RawMessage) (any, error) {
			p, err := DecodeParams[T](raw)
			if pr, ok := any(&p).(preparer); ok && err == nil {
				pr.prepare()
			}
			return p, err
		},
	}
}

func registerNested[T any](check func(chk, T), nested func(T) nesting) entry {
	e := register(check)
	e.nested = func(raw codec.RawMessage) [][]Action {
		p, err := DecodeParams[T](raw)
		if err != nil {
			return nil
		}
		return nested(p).lists
	}
	e.plan = func(params any) nesting { return nested(params.(T)) }
	return e
}

func paramsCode(err error) issueCode {
	if codec.IsUnknownField(err) {
		return CodeUnknownField
	}
	return CodeInvalidFormat
}

type typedParams struct {
	Type   string
	Params codec.RawMessage
}

func (c chk) typed(reg map[string]entry, t typedParams) {
	e, ok := reg[t.Type]
	switch {
	case t.Type == "":
		c.add("type", CodeRequired)
	case !ok:
		c.add("type", CodeUnknownType)
	default:
		e.check(c.sub("params"), t.Params)
	}
}

func (c chk) conditions(list []Condition) {
	for i, cond := range list {
		c.sub("conditions").elem(index(i)).typed(conditionRegistry, typedParams(cond))
	}
}

func (c chk) actions(list []Action) {
	for i, a := range list {
		c.action(index(i), a)
	}
}

func (c chk) action(i index, a Action) {
	at := c.elem(i)
	if c.triggerMismatch(a.Type) {
		at.add("type", CodeTriggerMismatch)
	}
	if c.permMissing(a.Type) {
		at.add("type", CodePermRequired)
	}
	at.typed(actionRegistry, typedParams{Type: a.Type, Params: a.Params})
}

// IsBranching reports a step that only routes to other steps, so it does not count toward MaxActions.
func IsBranching(typ string) bool { return actionRegistry[typ].nested != nil }

func (c chk) triggerMismatch(typ string) bool {
	want, ok := requiredTrigger[typ]
	return ok && c.trigger != want
}

func (c chk) permMissing(typ string) bool {
	role, ok := actionRole[typ]
	return ok && c.trigger == "command" && c.perm < role
}

func permRank(conditions []Condition) int {
	best := -1
	for _, cond := range conditions {
		if cond.Type != "perm" {
			continue
		}
		p, err := DecodeParams[PermCondition](cond.Params)
		if err == nil {
			best = max(best, slices.Index(permLevels, p.Level))
		}
	}
	return best
}

func (a Action) Nested() [][]Action {
	e, ok := actionRegistry[a.Type]
	if !ok || e.nested == nil {
		return nil
	}
	return e.nested(a.Params)
}

func init() {
	triggerRegistry = map[string]entry{
		"command": register(checkCommand),
		"event":   register(checkEvent),
		"timer":   register(checkTimer),
		"reward":  register(checkReward),
		"manual":  register(checkEmpty),
	}
	conditionRegistry = map[string]entry{
		"perm":      register(checkPerm),
		"users":     register(checkUsers),
		"live":      register(checkLive),
		"cooldown":  register(checkCooldown),
		"chance":    register(checkChance),
		"counter":   register(checkCounterCond),
		"points":    register(checkPointsCond),
		"arg_regex": register(checkArgRegex),
		"category":  register(checkCategory),
		"compare":   register(checkCompare),
	}
	actionRegistry = map[string]entry{
		"chat":           register(checkText),
		"announce":       register(checkAnnounce),
		"reply":          register(checkText),
		"timeout":        register(checkTimeout),
		"ban":            register(checkBan),
		"delete_message": register(checkEmpty),
		"warn":           register(checkWarn),
		"shield":         register(checkShield),
		"shoutout":       register(checkLogin),
		"clip":           register(checkEmpty),
		"marker":         register(checkMarker),
		"set_title":      register(checkSetTitle),
		"set_game":       register(checkSetGame),
		"set_tags":       register(checkSetTags),
		"commercial":     register(checkCommercial),
		"points_add":     register(checkPointsAdd),
		"points_set":     register(checkPointsSet),
		"counter_add":    register(checkCounterAdd),
		"counter_set":    register(checkCounterSet),
		"redemption":     register(checkRedemption),
		"reward_update":  register(checkRewardUpdate),
		"delay":          register(checkDelay),
		"random_pick":    registerNested(checkRandomPick, randomPickBranches),
		"parallel":       registerNested(checkParallel, parallelBranches),
		"run":            register(checkRun),
		"if":             registerNested(checkIf, ifBranches),
		"http_post":      register(checkHTTPPost),
		"lights":         register(checkLights),
		"stop":           register(checkEmpty),
	}
}
