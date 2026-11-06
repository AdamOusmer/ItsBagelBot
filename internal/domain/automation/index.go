// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package automation

import (
	"regexp"
	"slices"
	"strings"
	"unicode"
)

const (
	RewardRedemptionKind = "channel.channel_points_custom_reward_redemption.add"
	sourceChat           = "chat"
	sourceSubMessage     = "sub_message"
)

type CompiledTrigger struct {
	Plan     *Plan
	Command  CommandTrigger
	Filters  map[string]string
	Kind     EventKind
	RewardID string
	Left     *CooldownCondition

	scanNames, allNames []matchName
	regex               *regexp.Regexp
}

// TriggerIndex is immutable once compiled, so readers share it without copying.
type TriggerIndex struct {
	plans      map[string]*Plan
	commands   map[string][]*CompiledTrigger
	kinds      map[string][]*CompiledTrigger
	chatScan   []*CompiledTrigger
	subMessage []*CompiledTrigger
}

var triggerCompilers = map[string]func(*TriggerIndex, *Plan){
	"command": (*TriggerIndex).addCommand,
	"event":   (*TriggerIndex).addEvent,
	"reward":  (*TriggerIndex).addReward,
}

func CompileIndex(list []Automation) *TriggerIndex {
	ordered := runOrder(list)
	idx := &TriggerIndex{
		plans:    make(map[string]*Plan, len(ordered)),
		commands: map[string][]*CompiledTrigger{},
		kinds:    map[string][]*CompiledTrigger{},
	}
	for _, a := range ordered {
		plan := NewPlan(a)
		idx.plans[a.ID] = plan
		if add := triggerCompilers[a.Definition.Trigger.Type]; add != nil {
			add(idx, plan)
		}
		idx.addLeftCommands(plan)
	}
	return idx
}

func (i *TriggerIndex) Plan(id string) *Plan {
	if i == nil {
		return nil
	}
	return i.plans[id]
}

func runOrder(list []Automation) []Automation {
	enabled := slices.DeleteFunc(slices.Clone(list), func(a Automation) bool { return !a.Enabled })
	slices.SortStableFunc(enabled, func(a, b Automation) int {
		return strings.Compare(a.Name+"\x00"+a.ID, b.Name+"\x00"+b.ID)
	})
	return enabled
}

func (i *TriggerIndex) Command(name string) []*CompiledTrigger {
	if i == nil {
		return nil
	}
	return i.commands[strings.ToLower(name)]
}

func (i *TriggerIndex) Kind(kind string) []*CompiledTrigger {
	if i == nil {
		return nil
	}
	return i.kinds[kind]
}

func (i *TriggerIndex) ChatScan() []*CompiledTrigger {
	if i == nil {
		return nil
	}
	return i.chatScan
}

func (i *TriggerIndex) SubMessage() []*CompiledTrigger {
	if i == nil {
		return nil
	}
	return i.subMessage
}

func (i *TriggerIndex) addCommand(plan *Plan) {
	t, err := DecodeParams[CommandTrigger](plan.Automation.Definition.Trigger.Params)
	if err != nil {
		return
	}
	entry := &CompiledTrigger{Plan: plan, Command: t}
	entry.compileMatch()
	if slices.Contains(t.Sources, sourceChat) {
		i.addChatNames(entry)
	}
	if slices.Contains(t.Sources, sourceSubMessage) {
		i.subMessage = append(i.subMessage, entry)
	}
}

func (i *TriggerIndex) addChatNames(entry *CompiledTrigger) {
	scanned := false
	seen := map[string]bool{}
	for _, name := range CommandNames(entry.Command) {
		if !entry.Command.DispatchRouted(name) {
			scanned = true
			continue
		}
		key := strings.ToLower(strings.TrimPrefix(name, "!"))
		if !seen[key] {
			seen[key] = true
			i.commands[key] = append(i.commands[key], entry)
		}
	}
	if scanned {
		i.chatScan = append(i.chatScan, entry)
	}
}

type kindedTrigger[T any] struct {
	kindName func(T) string
	fill     func(*CompiledTrigger, T)
}

func addKinded[T any](i *TriggerIndex, plan *Plan, spec kindedTrigger[T]) {
	t, err := DecodeParams[T](plan.Automation.Definition.Trigger.Params)
	if err != nil {
		return
	}
	name := spec.kindName(t)
	kind, ok := LookupEventKind(name)
	if !ok {
		return
	}
	entry := &CompiledTrigger{Plan: plan, Kind: kind}
	spec.fill(entry, t)
	i.kinds[name] = append(i.kinds[name], entry)
}

func (i *TriggerIndex) addEvent(plan *Plan) {
	addKinded(i, plan, kindedTrigger[EventTrigger]{
		kindName: func(t EventTrigger) string { return t.Kind },
		fill:     func(e *CompiledTrigger, t EventTrigger) { e.Filters = t.Filters },
	})
}

func (i *TriggerIndex) addReward(plan *Plan) {
	addKinded(i, plan, kindedTrigger[RewardTrigger]{
		kindName: func(RewardTrigger) string { return RewardRedemptionKind },
		fill:     func(e *CompiledTrigger, t RewardTrigger) { e.RewardID = t.RewardID },
	})
}

func CommandNames(t CommandTrigger) []string {
	if t.Match == matchRegex {
		return []string{t.Name}
	}
	return append([]string{t.Name}, t.Aliases...)
}

// DispatchRouted reports whether the command stage owns the name; custom commands run
// first on that path, so such a name must never also match in the chat scan.
func (t CommandTrigger) DispatchRouted(name string) bool {
	if t.Match != "start" && t.Match != "exact" {
		return false
	}
	return len(name) > 1 && name[0] == '!' && !strings.ContainsFunc(name, unicode.IsSpace)
}
