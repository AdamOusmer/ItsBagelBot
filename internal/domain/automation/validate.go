// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package automation

import (
	"sort"
	"strings"

	"ItsBagelBot/pkg/codec"
)

const maxFlattenDepth = 4

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
	flat := def.Flatten()
	c.count("actions", quantity(len(flat)), upTo(MaxActions))
	if totalDelayMS(flat) > MaxDelayMS {
		c.add("actions", CodeDelayBudget)
	}
}

func totalDelayMS(actions []Action) int {
	total := 0
	for _, a := range actions {
		if a.Type != "delay" {
			continue
		}
		p, _ := DecodeParams[DelayAction](a.Params)
		total += p.Ms
	}
	return total
}

func (d Definition) Flatten() []Action { return flatten(d.Actions, 0) }

func flatten(list []Action, depth int) []Action {
	out := make([]Action, 0, len(list))
	for _, a := range list {
		out = append(out, a)
		if depth >= maxFlattenDepth {
			continue
		}
		for _, branch := range a.Nested() {
			out = append(out, flatten(branch, depth+1)...)
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
