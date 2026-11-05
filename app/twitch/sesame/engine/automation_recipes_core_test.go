// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"slices"
	"strconv"
	"sync"
	"testing"
	"time"

	"ItsBagelBot/internal/domain/automation"

	"github.com/stretchr/testify/require"
)

type autoVariables struct {
	mu      sync.Mutex
	values  map[string]string
	writes  []string
	limited bool
	getErr  error
}

func newAutoVariables() *autoVariables { return &autoVariables{values: map[string]string{}} }

func variableKey(owner VariableOwner, name string) string {
	return owner.Scope + "/" + strconv.FormatUint(owner.ViewerID, 10) + "/" + name
}

func (v *autoVariables) VariablesGet(_ context.Context, owner VariableOwner, names []string) (map[string]string, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.getErr != nil {
		return nil, v.getErr
	}
	out := map[string]string{}
	for _, name := range names {
		if value, ok := v.values[variableKey(owner, name)]; ok {
			out[name] = value
		}
	}
	return out, nil
}

func (v *autoVariables) VariableSet(_ context.Context, owner VariableOwner, name, value string) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.limited {
		return ErrVariableLimit
	}
	v.values[variableKey(owner, name)] = value
	v.writes = append(v.writes, name+"="+value)
	return nil
}

func (v *autoVariables) VariableAdd(_ context.Context, owner VariableOwner, name string, amount int64) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	next, _ := automation.VariableValue(v.values[variableKey(owner, name)]).Add(amount)
	v.values[variableKey(owner, name)] = next
	v.writes = append(v.writes, name+"+"+strconv.FormatInt(amount, 10))
	return nil
}

func (v *autoVariables) VariableDelete(_ context.Context, owner VariableOwner, name string) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	delete(v.values, variableKey(owner, name))
	return nil
}

func (v *autoVariables) writesSoFar() []string {
	v.mu.Lock()
	defer v.mu.Unlock()
	return slices.Clone(v.writes)
}

func (v *autoVariables) value(owner VariableOwner, name string) string {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.values[variableKey(owner, name)]
}

var channelVars = VariableOwner{BroadcasterID: 1, Scope: automation.VariableScopeChannel}

func (fx *autoFixture) awaitWrites(t *testing.T, count int) []string {
	t.Helper()
	require.Eventually(t, func() bool { return len(fx.vars.writesSoFar()) >= count }, 3*time.Second, 2*time.Millisecond, "writes: %v", fx.vars.writesSoFar())
	return fx.vars.writesSoFar()
}

func actions(list ...automation.Action) []automation.Action { return list }

func loopOf(times string, body ...automation.Action) automation.Action {
	return act("loop", obj{"times": times, "actions": body})
}

type (
	slotName     string
	stepTemplate string
)

func setVar(name slotName, value stepTemplate) automation.Action {
	return act("var_set", obj{"scope": "channel", "name": name, "value": value})
}

func addVar(name slotName) automation.Action {
	return act("var_add", obj{"scope": "channel", "name": name, "amount": 1})
}

func delayOf(ms int) automation.Action { return act("delay", obj{"ms": ms}) }

func parallelOf(paths ...[]automation.Action) automation.Action {
	branches := make([]obj, len(paths))
	for i, path := range paths {
		branches[i] = obj{"weight": 1, "actions": path}
	}
	return act("parallel", obj{"branches": branches})
}

func saveLocal(name slotName, value stepTemplate) automation.Action {
	return act("text", obj{"op": "trim", "input": value, "save_as": name})
}
