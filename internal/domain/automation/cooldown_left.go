// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package automation

import (
	"strings"

	"ItsBagelBot/pkg/tmpl"
)

const (
	CooldownTokenName    = "cooldown"
	CooldownLeftPayload  = "left"
	leftCommandMatchMode = "start"
	actionIf             = "if"
)

// NormalizeCommandName is the form two command names are compared in.
func NormalizeCommandName(name string) string {
	return strings.ToLower(strings.TrimPrefix(strings.TrimSpace(name), "!"))
}

// IsCooldownLeftToken reports the {cooldown:left} token.
func IsCooldownLeftToken(t tmpl.Token) bool {
	return t.Kind == tmpl.KindVar && t.Name == CooldownTokenName && t.Payload == CooldownLeftPayload
}

// LeftCommandNames lists time-left commands from cooldown conditions and countdown starts, including nested steps.
func LeftCommandNames(def Definition) []string {
	var names []string
	collect := func(name string) {
		if name != "" {
			names = append(names, name)
		}
	}
	for _, c := range def.Conditions {
		collect(cooldownLeftCommand(c))
	}
	for _, a := range def.Flatten() {
		collect(actionLeftCommand(a))
	}
	return names
}

func cooldownLeftCommand(c Condition) string {
	if c.Type != conditionCooldown {
		return ""
	}
	p, err := DecodeParams[CooldownCondition](c.Params)
	if err != nil {
		return ""
	}
	return p.LeftCommand
}

func actionLeftCommand(a Action) string {
	switch a.Type {
	case "countdown_start":
		p, err := DecodeParams[CountdownStartAction](a.Params)
		if err == nil {
			return p.LeftCommand
		}
	case actionIf:
		p, err := DecodeParams[IfAction](a.Params)
		if err == nil {
			return cooldownLeftCommand(p.Condition)
		}
	}
	return ""
}

// TriggerCommandNames lists the names a command trigger answers to, normalized; a pattern is not a name.
func TriggerCommandNames(def Definition) []string {
	if def.Trigger.Type != "command" {
		return nil
	}
	t, err := DecodeParams[CommandTrigger](def.Trigger.Params)
	if err != nil || t.Match == matchRegex {
		return nil
	}
	names := CommandNames(t)
	for i, name := range names {
		names[i] = NormalizeCommandName(name)
	}
	return names
}

func (c chk) leftCommands(def Definition) {
	taken := map[string]bool{}
	for _, name := range TriggerCommandNames(def) {
		taken[name] = true
	}
	for _, name := range LeftCommandNames(def) {
		key := NormalizeCommandName(name)
		if taken[key] {
			c.add("left_command", CodeCommandExists)
		}
		taken[key] = true
	}
}

// LeftCooldowns lists the enabled cooldowns of a plan that answer a time left command.
func (p *Plan) LeftCooldowns() []CooldownCondition {
	var found []CooldownCondition
	collect := func(c *PlannedCondition) {
		if cooldown, ok := c.Params.(CooldownCondition); ok && cooldown.LeftCommand != "" {
			found = append(found, cooldown)
		}
	}
	for i := range p.Conditions {
		collect(&p.Conditions[i])
	}
	var walk func(steps []Step)
	walk = func(steps []Step) {
		for i := range steps {
			s := &steps[i]
			if !s.Enabled {
				continue
			}
			if s.Condition != nil {
				collect(s.Condition)
			}
			for _, branch := range s.Branches {
				walk(branch)
			}
		}
	}
	walk(p.Steps)
	return found
}

func (i *TriggerIndex) addLeftCommands(plan *Plan) {
	for _, cooldown := range plan.LeftCooldowns() {
		left := cooldown
		entry := &CompiledTrigger{
			Plan:    plan,
			Left:    &left,
			Command: CommandTrigger{Name: cooldown.LeftCommand, Match: leftCommandMatchMode, Sources: []string{sourceChat}},
		}
		entry.compileMatch()
		i.addChatNames(entry)
	}
}
