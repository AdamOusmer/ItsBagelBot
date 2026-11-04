// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package automation

import (
	"sort"
	"strings"

	"ItsBagelBot/pkg/codec"
)

func Parse(raw []byte) (Definition, []Issue) {
	if len(raw) > MaxDefinitionBytes {
		return Definition{}, []Issue{{Code: CodeTooLong}}
	}
	var def Definition
	if err := codec.UnmarshalStrict(raw, &def); err != nil {
		code := CodeInvalidJSON
		if codec.IsUnknownField(err) {
			code = CodeUnknownField
		}
		return Definition{}, []Issue{{Code: code}}
	}
	return def, Validate(def)
}

func Validate(def Definition) []Issue {
	var out []Issue
	c := chk{out: &out, trigger: def.Trigger.Type, perm: permRank(def.Conditions)}
	if def.Version != Version {
		c.add("version", CodeUnsupportedVer)
	}
	c.sub("trigger").typed(triggerRegistry, typedParams(def.Trigger))
	c.conditions(def.Conditions)
	c.sub("actions").actions(def.Actions)
	c.budgets(def)
	return finish(out)
}

func (c chk) budgets(def Definition) {
	c.count("actions", quantity(countEffects(def.Flatten())), upTo(MaxActions))
	if pathDelayMS(def.Actions) > MaxDelayMS {
		c.add("actions", CodeDelayBudget)
	}
}

func countEffects(flat []Action) int {
	n := 0
	for _, a := range flat {
		if !IsBranching(a.Type) {
			n++
		}
	}
	return n
}

func pathDelayMS(actions []Action) int {
	total := 0
	for _, a := range actions {
		total += actionDelayMS(a)
	}
	return total
}

func actionDelayMS(a Action) int {
	if a.Type == "delay" {
		p, _ := DecodeParams[DelayAction](a.Params)
		return p.Ms
	}
	longest := 0
	for _, branch := range a.Nested() {
		longest = max(longest, pathDelayMS(branch))
	}
	return longest
}

func (d Definition) Flatten() []Action { return flatten(d.Actions) }

func flatten(list []Action) []Action {
	out := make([]Action, 0, len(list))
	for _, a := range list {
		out = append(out, a)
		for _, branch := range a.Nested() {
			out = append(out, flatten(branch)...)
		}
	}
	return out
}

func ValidateName(name string) []Issue {
	var out []Issue
	c := chk{out: &out}
	s := strings.TrimSpace(name)
	if c.text("name", s, nonEmpty(MaxNameLength)) {
		c.chars("name", s, isNameChar)
	}
	return finish(out)
}

func finish(issues []Issue) []Issue {
	if len(issues) == 0 {
		return nil
	}
	sort.Slice(issues, func(i, j int) bool {
		if issues[i].Path != issues[j].Path {
			return issues[i].Path < issues[j].Path
		}
		return issues[i].Code < issues[j].Code
	})
	out := issues[:1]
	for _, is := range issues[1:] {
		if is != out[len(out)-1] {
			out = append(out, is)
		}
	}
	return out
}
