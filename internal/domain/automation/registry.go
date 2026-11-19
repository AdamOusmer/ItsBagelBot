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
	effect bool
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
const roleLeadModerator = 4

var requiredTrigger = map[string]string{
	"delete_message": "command",
	"redemption":     "reward",
}

type triggerShape struct{ typ, kind string }

var viewerlessTriggers = map[triggerShape]struct{}{
	{typ: "countdown"}:                     {},
	{typ: "timer"}:                         {},
	{typ: "event", kind: "stream.online"}:  {},
	{typ: "event", kind: "stream.offline"}: {},
}

var actionRole = map[string]int{
	"poll":            roleModerator,
	"poll_end":        roleModerator,
	"prediction":      roleModerator,
	"prediction_end":  roleModerator,
	"raid":            roleLeadModerator,
	"raid_cancel":     roleLeadModerator,
	"mod":             roleLeadModerator,
	"vip":             roleLeadModerator,
	"chat_mode":       roleModerator,
	"clear_chat":      roleModerator,
	"ad_snooze":       roleLeadModerator,
	"whisper":         roleModerator,
	"unban_resolve":   roleModerator,
	"branded_content": roleLeadModerator,
	"set_title":       roleLeadModerator,
	"set_game":        roleLeadModerator,
	"set_tags":        roleLeadModerator,
	"commercial":      roleLeadModerator,
	"marker":          roleLeadModerator,
	"clip":            roleLeadModerator,

	"timeout": roleModerator, "ban": roleModerator, "unban": roleModerator, "warn": roleModerator, "shield": roleModerator,
	"delete_message": roleModerator, "cooldown_add": roleModerator,
	"countdown_start": roleModerator, "countdown_add": roleModerator,
}

var selfTargetExempt = map[string]func(codec.RawMessage) bool{
	"timeout":        whoIsViewer,
	"ban":            whoIsViewer,
	"delete_message": func(codec.RawMessage) bool { return true },
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

func registerNestedEffect[T any](check func(chk, T), nested func(T) nesting) entry {
	e := registerNested(check, nested)
	e.effect = true
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
	if c.triggerMismatch(a) {
		at.add("type", CodeTriggerMismatch)
	}
	if c.permMissing(a) {
		at.add("type", CodePermRequired)
	}
	at.typed(actionRegistry, typedParams{Type: a.Type, Params: a.Params})
}

// IsBranching reports a step that only routes to other steps, so it does not count toward MaxActions.
func IsBranching(typ string) bool {
	e := actionRegistry[typ]
	return e.nested != nil && !e.effect
}

func (c chk) triggerMismatch(a Action) bool {
	switch a.Type {
	case "unban_resolve":
		return c.trigger != "event" || c.kind != "channel.unban_request.create"
	case "points_add", "points_set":
		_, viewerless := viewerlessTriggers[triggerShape{typ: c.trigger, kind: c.kind}]
		return viewerless && pointsTargetViewer(a.Params)
	}
	want, ok := requiredTrigger[a.Type]
	return ok && c.trigger != want
}

func pointsTargetViewer(raw codec.RawMessage) bool {
	p, err := DecodeParams[PointsAddAction](raw)
	if err != nil {
		return false
	}
	return p.targetsViewer()
}

func whoIsViewer(raw codec.RawMessage) bool {
	var p struct {
		Who string `json:"who"`
	}
	return codec.Unmarshal(raw, &p) == nil && p.Who == "viewer"
}

func (c chk) permMissing(a Action) bool {
	role, ok := actionRole[a.Type]
	if !ok || c.trigger != "command" {
		return false
	}
	return c.perm < role && !selfTargeted(a)
}

func selfTargeted(a Action) bool {
	exempt, ok := selfTargetExempt[a.Type]
	return ok && exempt(a.Params)
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
		"command":   register(checkCommand),
		"event":     register(checkEvent),
		"timer":     register(checkTimer),
		"reward":    register(checkReward),
		"manual":    register(checkEmpty),
		"countdown": register(checkCountdownTrigger),
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
		"var":       register(checkVarCondition),
	}
	actionRegistry = map[string]entry{
		"poll":            register(checkPoll),
		"poll_end":        register(checkPollEnd),
		"prediction":      register(checkPrediction),
		"prediction_end":  register(checkPredictionEnd),
		"raid":            register(checkLogin),
		"raid_cancel":     register(checkEmpty),
		"mod":             register(checkTargetToggle),
		"vip":             register(checkTargetToggle),
		"chat_mode":       register(checkChatMode),
		"clear_chat":      register(checkEmpty),
		"ad_snooze":       register(checkEmpty),
		"whisper":         register(checkWhisper),
		"unban_resolve":   register(checkUnbanResolve),
		"branded_content": register(checkBrandedContent),

		"chat":            register(checkText),
		"announce":        register(checkAnnounce),
		"reply":           register(checkText),
		"timeout":         register(checkTimeout),
		"ban":             register(checkBan),
		"unban":           register(checkUnban),
		"delete_message":  register(checkEmpty),
		"warn":            register(checkWarn),
		"shield":          register(checkShield),
		"shoutout":        register(checkLogin),
		"clip":            register(checkEmpty),
		"marker":          register(checkMarker),
		"set_title":       register(checkSetTitle),
		"set_game":        register(checkSetGame),
		"set_tags":        register(checkSetTags),
		"commercial":      register(checkCommercial),
		"points_add":      register(checkPointsAdd),
		"points_set":      register(checkPointsSet),
		"counter_add":     register(checkCounterAdd),
		"counter_set":     register(checkCounterSet),
		"redemption":      register(checkRedemption),
		"reward_update":   register(checkRewardUpdate),
		"delay":           register(checkDelay),
		"random_pick":     registerNested(checkRandomPick, randomPickBranches),
		"parallel":        registerNested(checkParallel, parallelBranches),
		"run":             register(checkRun),
		"if":              registerNested(checkIf, ifBranches),
		"loop":            registerNested(checkLoop, loopBranches),
		"http_post":       registerNestedEffect(checkHTTPPost, httpPostBranches),
		"http_get":        registerNestedEffect(checkHTTPGet, httpGetBranches),
		"lights":          register(checkLights),
		"stop":            register(checkEmpty),
		"var_set":         register(checkVarSet),
		"var_add":         register(checkVarAdd),
		"var_delete":      register(checkVarDelete),
		"text":            register(checkTextStep),
		"math":            register(checkMath),
		"cooldown_add":    register(checkCooldownAdd),
		"countdown_start": register(checkCountdownStart),
		"countdown_add":   register(checkCountdownAdd),
	}
}

func eventKindOf(t Trigger) string {
	if t.Type != "event" {
		return ""
	}
	p, err := DecodeParams[EventTrigger](t.Params)
	if err != nil {
		return ""
	}
	return p.Kind
}
