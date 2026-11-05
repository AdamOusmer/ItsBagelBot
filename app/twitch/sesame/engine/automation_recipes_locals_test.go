// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"testing"

	"ItsBagelBot/internal/domain/automation"

	"github.com/stretchr/testify/assert"
)

func TestLocalsSurviveADelay(t *testing.T) {
	fx := newAutoFixture(manual("main", saveLocal("x", "kept {arg:a}"), delayOf(1), setVar("out", "{local:x}|{local:X}|{local:missing}")))
	fx.run(t, "main", "req-local-delay", map[string]string{"a": "1"})
	assert.Equal(t, []string{"out=kept 1|kept 1|"}, fx.vars.writesSoFar())
}

func TestLocalsCopyIntoEachParallelPath(t *testing.T) {
	fx := newAutoFixture(manual("main",
		saveLocal("x", "root"),
		parallelOf(
			actions(saveLocal("x", "first"), delayOf(1), setVar("p1", "{local:x}")),
			actions(setVar("p2", "{local:x}"), saveLocal("x", "second"), setVar("p2b", "{local:x}")),
		),
		setVar("after", "{local:x}"),
	))
	fx.run(t, "main", "req-local-par", nil)
	assert.ElementsMatch(t, []string{"p1=first", "p2=root", "p2b=second", "after=root"}, fx.awaitWrites(t, 4))
}

func TestLocalsFlowDownIntoRunChildrenAndNotBack(t *testing.T) {
	fx := newAutoFixture(
		manual("main", saveLocal("x", "parent"), act("run", obj{"automation_id": "child"}), setVar("back", "{local:x}|{local:fromchild}")),
		manual("child", setVar("seen", "{local:x}"), saveLocal("fromchild", "y"), saveLocal("x", "child")),
	)
	fx.run(t, "main", "req-local-run", nil)
	assert.Equal(t, []string{"seen=parent", "back=parent|"}, fx.vars.writesSoFar())
}

func TestStoredContentIsNeverExpandedAgain(t *testing.T) {
	const hostile = "{arg:secret} {var:other} {local:secret} {user}"
	args := map[string]string{"evil": hostile, "secret": "LEAK"}
	cases := map[string][]automation.Action{
		"channel variable": actions(setVar("x", "{arg:evil}"), act("chat", obj{"text": "{var:x}"})),
		"viewer variable": actions(
			act("var_set", obj{"scope": "viewer", "name": "x", "value": "{arg:evil}"}),
			act("chat", obj{"text": "{viewervar:x}"}),
		),
		"variable copied to a variable": actions(setVar("x", "{arg:evil}"), setVar("y", "{var:x}"), act("chat", obj{"text": "{var:y}"})),
		"local":                         actions(saveLocal("x", "{arg:evil}"), act("chat", obj{"text": "{local:x}"})),
		"local kept in a variable":      actions(saveLocal("x", "{arg:evil}"), setVar("y", "{local:x}"), act("chat", obj{"text": "{var:y}"})),
		"local across a delay":          actions(saveLocal("x", "{arg:evil}"), delayOf(1), act("chat", obj{"text": "{local:x}"})),
		"fetch result": actions(
			act("http_get", obj{"url": "https://example.com/x", "save_as": "x"}),
			act("chat", obj{"text": "{local:x}"}),
		),
	}
	for name, steps := range cases {
		t.Run(name, func(t *testing.T) {
			fx := newAutoFixture(manual("main", steps...))
			fx.gossip.values = []string{hostile}
			fx.run(t, "main", "req-reexpand", args)
			assert.Equal(t, []string{hostile}, fx.pub.chatTexts(t))
		})
	}
}

func TestStoredTemplateIsLiteralInConditionsToo(t *testing.T) {
	branch := act("if", obj{
		"condition": obj{"type": "var", "params": obj{"scope": "channel", "name": "x", "op": "eq", "value": "{arg:evil}"}},
		"then":      actions(setVar("result", "same")),
		"else":      actions(setVar("result", "different")),
	})
	fx := newAutoFixture(manual("main", setVar("x", "{arg:evil}"), branch))
	fx.run(t, "main", "req-cond-literal", map[string]string{"evil": "{arg:secret}", "secret": "LEAK"})
	assert.Equal(t, "same", fx.vars.value(channelVars, "result"))
	assert.Equal(t, "{arg:secret}", fx.vars.value(channelVars, "x"))
}
