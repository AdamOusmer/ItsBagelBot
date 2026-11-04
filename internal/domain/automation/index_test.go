// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package automation_test

import (
	"testing"

	"ItsBagelBot/internal/domain/automation"
	"ItsBagelBot/pkg/codec"

	"github.com/stretchr/testify/assert"
)

func compiled(id, name, typ, params string) automation.Automation {
	return automation.Automation{
		ID: id, Name: name, Enabled: true,
		Definition: automation.Definition{Version: 1, Trigger: automation.Trigger{Type: typ, Params: codec.RawMessage(params)}},
	}
}

func disabled(a automation.Automation) automation.Automation {
	a.Enabled = false
	return a
}

func ids(list []*automation.CompiledTrigger) []string {
	out := make([]string, 0, len(list))
	for _, t := range list {
		out = append(out, t.Plan.Automation.ID)
	}
	return out
}

func TestCompileIndexRoutesEachTriggerOnce(t *testing.T) {
	idx := automation.CompileIndex([]automation.Automation{
		compiled("z", "z", "command", `{"name":"!hug","aliases":["!HUG","!cuddle"]}`),
		compiled("a", "a", "command", `{"name":"!hug"}`),
		disabled(compiled("off", "b", "command", `{"name":"!hug"}`)),
		compiled("bad", "c", "command", `{"name":"!hug","nope":1}`),
		compiled("re", "d", "command", `{"name":"h.g","match":"regex"}`),
		compiled("any", "e", "command", `{"name":"hello","match":"anywhere"}`),
		compiled("mixed", "f", "command", `{"name":"!mix","aliases":["no prefix"]}`),
		compiled("sub", "g", "command", `{"name":"!sub","sources":["sub_message"]}`),
		compiled("evt", "h", "event", `{"kind":"channel.follow"}`),
		compiled("unknown", "i", "event", `{"kind":"not.a.kind"}`),
		compiled("rew", "j", "reward", `{"reward_id":"r1"}`),
		compiled("man", "k", "manual", `{}`),
	})

	assert.Equal(t, []string{"a", "z"}, ids(idx.Command("HuG")))
	assert.Equal(t, []string{"z"}, ids(idx.Command("cuddle")))
	assert.Equal(t, []string{"mixed"}, ids(idx.Command("mix")))
	assert.Empty(t, idx.Command("sub"))
	assert.Equal(t, []string{"re", "any", "mixed"}, ids(idx.ChatScan()))
	assert.Equal(t, []string{"sub"}, ids(idx.SubMessage()))
	assert.Equal(t, []string{"evt"}, ids(idx.Kind("channel.follow")))
	assert.Empty(t, idx.Kind("not.a.kind"))
	assert.Equal(t, []string{"rew"}, ids(idx.Kind(automation.RewardRedemptionKind)))
}

func TestNilIndexIsEmpty(t *testing.T) {
	var idx *automation.TriggerIndex
	assert.Empty(t, idx.Command("hug"))
	assert.Empty(t, idx.Kind("channel.follow"))
	assert.Empty(t, idx.ChatScan())
	assert.Empty(t, idx.SubMessage())
}
