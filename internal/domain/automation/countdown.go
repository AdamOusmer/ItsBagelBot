// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package automation

import "ItsBagelBot/pkg/tmpl"

// CountdownTrigger fires once a named channel countdown finishes. A countdown is
// independent of the cooldown conditions that gate recipe execution.
type CountdownTrigger struct {
	Name string `json:"name"`
}

// CountdownStartAction starts or restarts a named channel countdown.
type CountdownStartAction struct {
	Name        string `json:"name"`
	Seconds     int    `json:"seconds"`
	LeftCommand string `json:"left_command,omitempty"`
	LeftReply   string `json:"left_reply,omitempty"`
	ReadyReply  string `json:"ready_reply,omitempty"`
}

type CountdownAddAction struct {
	Name    string `json:"name"`
	Seconds int    `json:"seconds"`
	Per     int    `json:"per,omitempty"`
}

func checkCountdownTrigger(c chk, p CountdownTrigger) { c.variableName("name", p.Name) }

func checkCountdownStart(c chk, p CountdownStartAction) {
	c.variableName("name", p.Name)
	inRange(c, "seconds", p.Seconds, between(1, MaxCooldownSeconds))
	if p.LeftCommand != "" {
		c.commandName("left_command", p.LeftCommand, "start")
	}
	c.text("left_reply", p.LeftReply, upTo(MaxTemplateLength))
	c.text("ready_reply", p.ReadyReply, upTo(MaxTemplateLength))
}

func checkCountdownAdd(c chk, p CountdownAddAction) {
	checkCooldownAdd(c, CooldownAddAction{Name: p.Name, Seconds: p.Seconds, Per: p.Per})
}

// CountdownAddSeconds counts whole event amounts without carrying a remainder.
func CountdownAddSeconds(p CountdownAddAction, amount int64) (int64, bool) {
	if p.Seconds <= 0 {
		return 0, false
	}
	if p.Per <= 0 {
		return int64(p.Seconds), true
	}
	multiples := amount / int64(p.Per)
	if multiples <= 0 {
		return 0, false
	}
	// More than the maximum window has the same result at the store. Bound
	// before multiplying so a large event amount cannot overflow int64.
	if multiples > int64(MaxCooldownSeconds)/int64(p.Seconds) {
		return MaxCooldownSeconds, true
	}
	return int64(p.Seconds) * multiples, true
}

func IsCountdownLeftToken(t tmpl.Token) bool {
	return t.Kind == tmpl.KindVar && t.Name == "countdown" && t.Payload == "left"
}

// OwnedCountdownNames lists every countdown a definition can start, including
// nested actions, for name choices and portable recipe imports.
func OwnedCountdownNames(def Definition) []string {
	var names []string
	for _, a := range def.Flatten() {
		if a.Type != "countdown_start" {
			continue
		}
		p, err := DecodeParams[CountdownStartAction](a.Params)
		if name, valid := VariableName(p.Name); err == nil && valid {
			names = append(names, name)
		}
	}
	return names
}

// LeftCountdowns returns enabled starts whose named countdown has a chat query.
func (p *Plan) LeftCountdowns() []CountdownStartAction {
	var found []CountdownStartAction
	var walk func([]Step)
	walk = func(steps []Step) {
		for _, s := range steps {
			if !s.Enabled {
				continue
			}
			if start, ok := s.Params.(CountdownStartAction); ok && start.LeftCommand != "" {
				found = append(found, start)
			}
			for _, branch := range s.Branches {
				walk(branch)
			}
		}
	}
	walk(p.Steps)
	return found
}

func (i *TriggerIndex) addCountdownLeftCommands(plan *Plan) {
	for _, start := range plan.LeftCountdowns() {
		left := start
		entry := &CompiledTrigger{
			Plan: plan, CountdownLeft: &left,
			Command: CommandTrigger{Name: start.LeftCommand, Match: leftCommandMatchMode, Sources: []string{sourceChat}},
		}
		entry.compileMatch()
		i.addChatNames(entry)
	}
}

func (i *TriggerIndex) addCountdown(plan *Plan) {
	p, err := DecodeParams[CountdownTrigger](plan.Automation.Definition.Trigger.Params)
	name, valid := VariableName(p.Name)
	if err != nil || !valid {
		return
	}
	i.countdowns[name] = append(i.countdowns[name], &CompiledTrigger{Plan: plan})
}

func (i *TriggerIndex) Countdown(name string) []*CompiledTrigger {
	if i == nil {
		return nil
	}
	key, valid := VariableName(name)
	if !valid {
		return nil
	}
	return i.countdowns[key]
}
