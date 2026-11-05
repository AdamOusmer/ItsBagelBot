// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

type actionHandler func(x *actionExec, s step) error

type actionGroup func(*actionTableBuilder) *actionTableBuilder

// The table cannot be a var initializer: flow handlers reach dispatch, which reads it.
var actionTable map[string]actionHandler

func init() {
	actionTable = newActionTable().
		Steps(outputActions).
		Steps(effectActions).
		Steps(flowActions).
		build()
}

type actionTableBuilder struct {
	handlers map[string]actionHandler
}

func newActionTable() *actionTableBuilder {
	return &actionTableBuilder{handlers: map[string]actionHandler{}}
}

func (b *actionTableBuilder) Step(name string, h actionHandler) *actionTableBuilder {
	if _, dup := b.handlers[name]; dup {
		panic("sesame/engine: duplicate action " + name)
	}
	b.handlers[name] = h
	return b
}

func (b *actionTableBuilder) Steps(group actionGroup) *actionTableBuilder {
	return group(b)
}

func (b *actionTableBuilder) build() map[string]actionHandler {
	return b.handlers
}

func typed[T any](h func(*actionExec, step, T) error) actionHandler {
	return func(x *actionExec, s step) error {
		params, ok := s.Params.(T)
		if !ok {
			return failure("bad_params")
		}
		return h(x, s, params)
	}
}

func outputActions(b *actionTableBuilder) *actionTableBuilder {
	return b.
		Step("chat", typed((*actionExec).chat)).
		Step("announce", typed((*actionExec).announce)).
		Step("reply", typed((*actionExec).reply)).
		Step("timeout", typed((*actionExec).timeout)).
		Step("ban", typed((*actionExec).ban)).
		Step("warn", typed((*actionExec).warn)).
		Step("delete_message", typed((*actionExec).deleteMessage)).
		Step("shield", typed((*actionExec).shield)).
		Step("shoutout", typed((*actionExec).shoutout)).
		Step("clip", typed((*actionExec).clip)).
		Step("marker", typed((*actionExec).marker)).
		Step("set_title", typed((*actionExec).setTitle)).
		Step("set_game", typed((*actionExec).setGame)).
		Step("set_tags", typed((*actionExec).setTags)).
		Step("commercial", typed((*actionExec).commercial)).
		Step("reward_update", typed((*actionExec).rewardUpdate)).
		Step("redemption", typed((*actionExec).redemption))
}

func effectActions(b *actionTableBuilder) *actionTableBuilder {
	return b.
		Step("points_add", typed((*actionExec).pointsAdd)).
		Step("points_set", typed((*actionExec).pointsSet)).
		Step("counter_add", typed((*actionExec).counterAdd)).
		Step("counter_set", typed((*actionExec).counterSet)).
		Step("http_post", typed((*actionExec).httpPost)).
		Step("http_get", typed((*actionExec).httpGet)).
		Step("lights", typed((*actionExec).lights)).
		Step("var_set", typed((*actionExec).varSet)).
		Step("var_add", typed((*actionExec).varAdd)).
		Step("var_delete", typed((*actionExec).varDelete)).
		Step("text", typed((*actionExec).textStep)).
		Step("math", typed((*actionExec).mathStep)).
		Step("overlay_show", typed((*actionExec).overlayShow)).
		Step("overlay_sound", typed((*actionExec).overlaySound))
}

func flowActions(b *actionTableBuilder) *actionTableBuilder {
	return b.
		Step("delay", typed((*actionExec).delay)).
		Step("random_pick", typed((*actionExec).randomPick)).
		Step("parallel", typed((*actionExec).parallel)).
		Step("loop", typed((*actionExec).loop)).
		Step("run", typed((*actionExec).runOther)).
		Step("if", typed((*actionExec).ifElse)).
		Step("stop", typed((*actionExec).stop))
}
