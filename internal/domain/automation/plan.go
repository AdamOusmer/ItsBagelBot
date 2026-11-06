// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package automation

import (
	"reflect"
	"slices"
	"strconv"

	"ItsBagelBot/pkg/tmpl"
)

const conditionCooldown = "cooldown"

type Plan struct {
	Automation Automation
	Conditions []PlannedCondition
	Steps      []Step
	StepCount  int
	Templates  map[string][]tmpl.Token
	// A refused if only spends a lookup on the time left when a template can show it.
	ShowsCooldownLeft bool
}

type PlannedCondition struct {
	Type   string
	Params any
	Err    error
	Reason string
}

type Step struct {
	Type      string
	Enabled   bool
	Params    any
	Index     int
	EffectKey string
	Branches  [][]Step
	Condition *PlannedCondition
}

type planner struct {
	id        string
	count     int
	templates map[string][]tmpl.Token

	showsCooldownLeft bool
}

func NewPlan(a Automation) *Plan {
	pl := &planner{id: a.ID}
	steps := pl.steps(a.Definition.Actions, "", -1)
	conditions := pl.conditions(a.Definition.Conditions)
	return &Plan{
		Automation: a,
		Conditions: conditions,
		Steps:      steps,
		StepCount:  pl.count,
		Templates:  pl.templates,

		ShowsCooldownLeft: pl.showsCooldownLeft,
	}
}

func (pl *planner) conditions(list []Condition) []PlannedCondition {
	if len(list) == 0 {
		return nil
	}
	out := make([]PlannedCondition, 0, len(list))
	var cooldowns []PlannedCondition
	for i, c := range list {
		planned := pl.condition(c)
		planned.Reason = "condition:" + strconv.Itoa(i) + ":" + c.Type
		if c.Type == conditionCooldown {
			cooldowns = append(cooldowns, planned)
			continue
		}
		out = append(out, planned)
	}
	return slices.Concat(out, cooldowns)
}

func (pl *planner) condition(c Condition) PlannedCondition {
	planned := PlannedCondition{Type: c.Type}
	e, ok := conditionRegistry[c.Type]
	if !ok || e.decode == nil {
		return planned
	}
	planned.Params, planned.Err = e.decode(c.Params)
	if planned.Err != nil {
		planned.Params = nil
	}
	if p, ok := planned.Params.(CompareCondition); ok {
		pl.lexTemplate(p.Left)
		pl.lexTemplate(p.Right)
	}
	return planned
}

func (pl *planner) steps(list []Action, prefix string, top int) []Step {
	if len(list) == 0 {
		return nil
	}
	out := make([]Step, len(list))
	for i, a := range list {
		out[i] = pl.step(a, prefix+strconv.Itoa(i), top, i)
	}
	return out
}

func (pl *planner) step(a Action, path string, top, i int) Step {
	pl.count++
	if top < 0 {
		top = i
	}
	s := Step{Type: a.Type, Enabled: a.IsEnabled(), Index: top, EffectKey: "auto:" + pl.id + ":" + path}
	e, params, ok := decodeAction(a)
	if !ok {
		return s
	}
	s.Params = params
	if e.plan != nil {
		pl.nest(&s, path, e.plan(params))
		return s
	}
	pl.lexTemplates(reflect.ValueOf(params))
	return s
}

func decodeAction(a Action) (entry, any, bool) {
	e, ok := actionRegistry[a.Type]
	if !ok || e.decode == nil {
		return e, nil, false
	}
	params, err := e.decode(a.Params)
	return e, params, err == nil
}

func (pl *planner) lexTemplates(v reflect.Value) {
	switch v.Kind() {
	case reflect.String:
		pl.lexTemplate(v.String())
	case reflect.Pointer, reflect.Interface:
		pl.lexIndirect(v)
	case reflect.Slice, reflect.Array:
		pl.lexElements(v)
	case reflect.Struct:
		pl.lexFields(v)
	}
}

func (pl *planner) lexIndirect(v reflect.Value) {
	if !v.IsNil() {
		pl.lexTemplates(v.Elem())
	}
}

func (pl *planner) lexElements(v reflect.Value) {
	for i := range v.Len() {
		pl.lexTemplates(v.Index(i))
	}
}

func (pl *planner) lexFields(v reflect.Value) {
	for i := range v.NumField() {
		pl.lexTemplates(v.Field(i))
	}
}

func (pl *planner) lexTemplate(text string) {
	toks := tmpl.Lex(text)
	if !slices.ContainsFunc(toks, func(t tmpl.Token) bool { return t.Kind == tmpl.KindVar }) {
		return
	}
	pl.showsCooldownLeft = pl.showsCooldownLeft || slices.ContainsFunc(toks, IsCooldownLeftToken)
	if pl.templates == nil {
		pl.templates = make(map[string][]tmpl.Token)
	}
	pl.templates[text] = toks
}

func (pl *planner) nest(s *Step, path string, n nesting) {
	s.Branches = make([][]Step, len(n.lists))
	for k, list := range n.lists {
		s.Branches[k] = pl.steps(list, path+"."+n.labels[k]+".", s.Index)
	}
	if n.condition != nil {
		c := pl.condition(*n.condition)
		s.Condition = &c
	}
}
