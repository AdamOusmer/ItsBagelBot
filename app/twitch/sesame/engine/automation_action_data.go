// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"errors"

	"ItsBagelBot/internal/domain/automation"
	automationrpc "ItsBagelBot/internal/domain/rpc/automation"
)

const (
	localIndex = "index"
	localError = "error"
)

func (x *actionExec) setLocal(name, value string) {
	if x.run.Locals == nil {
		x.run.Locals = make(map[string]string, 4)
	}
	x.run.Locals[name] = automation.TruncateRunes(value, automation.MaxSavedValueRunes)
}

func (x *actionExec) saveLocal(rawName, value string) {
	if name, ok := automation.VariableName(rawName); ok {
		x.setLocal(name, value)
	}
}

func (x *actionExec) variableOwner(scopeName string) (VariableOwner, error) {
	owner := VariableOwner{BroadcasterID: x.run.BroadcasterID, Scope: scopeName}
	if scopeName != automation.VariableScopeViewer {
		return owner, nil
	}
	if x.run.Viewer.ID == 0 {
		return owner, failure("no_viewer")
	}
	owner.ViewerID = x.run.Viewer.ID
	return owner, nil
}

func (x *actionExec) variableWrite(s step, scopeName string, write func(VariableStore, VariableOwner) error) error {
	store := x.p.automations.Variables
	if store == nil {
		return failure("no_variables")
	}
	owner, err := x.variableOwner(scopeName)
	if err != nil {
		return err
	}
	return x.once(s, func() error { return variableResult(write(store, owner)) })
}

func variableResult(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, ErrVariableLimit):
		return limited("var_limit")
	}
	return failure("store_error")
}

func (x *actionExec) varSet(s step, p automation.VarSetAction) error {
	name, _ := automation.VariableName(p.Name)
	value := x.expand(p.Value)
	if !automation.ValidVariableValue(value) {
		return limited("var_limit")
	}
	return x.variableWrite(s, p.Scope, func(store VariableStore, owner VariableOwner) error {
		return store.VariableSet(x.ctx, owner, name, value)
	})
}

func (x *actionExec) varAdd(s step, p automation.VarAddAction) error {
	name, _ := automation.VariableName(p.Name)
	return x.variableWrite(s, p.Scope, func(store VariableStore, owner VariableOwner) error {
		return store.VariableAdd(x.ctx, owner, name, p.Amount)
	})
}

func (x *actionExec) varDelete(s step, p automation.VarDeleteAction) error {
	name, _ := automation.VariableName(p.Name)
	return x.variableWrite(s, p.Scope, func(store VariableStore, owner VariableOwner) error {
		return store.VariableDelete(x.ctx, owner, name)
	})
}

func (x *actionExec) conditionVar(c automation.VarCondition) (bool, error) {
	store := x.p.automations.Variables
	if store == nil {
		return false, nil
	}
	owner, err := x.variableOwner(c.Scope)
	if err != nil {
		return false, nil
	}
	name, _ := automation.VariableName(c.Name)
	values, err := store.VariablesGet(x.ctx, owner, []string{name})
	if err != nil {
		return false, err
	}
	current, exists := values[name]
	return automation.VarPasses(c.Op, current, x.expand(c.Value), exists), nil
}

func (x *actionExec) textStep(_ step, p automation.TextStepAction) error {
	operands := automation.TextOperands{Input: x.expand(p.Input), Arg1: x.expand(p.Arg1), Arg2: x.expand(p.Arg2)}
	result, err := automation.ApplyText(p.Op, operands, x.p.automationIntN)
	if err != nil {
		return failure(err.Error())
	}
	x.saveLocal(p.SaveAs, result)
	return nil
}

func (x *actionExec) mathStep(_ step, p automation.MathAction) error {
	result, err := automation.EvalMath(x.expand(p.Expression), x.randomUnit)
	if err != nil {
		return failure(err.Error())
	}
	x.saveLocal(p.SaveAs, result)
	return nil
}

const randomUnitSteps = 1 << 53

func (x *actionExec) randomUnit() float64 {
	return float64(x.p.automationIntN(randomUnitSteps)) / randomUnitSteps
}

type fetchResult struct {
	failure string
	value   string
}

func replyResult(status string, values []string) fetchResult {
	if status != "ok" {
		return fetchResult{failure: status}
	}
	if len(values) == 0 {
		return fetchResult{}
	}
	return fetchResult{value: values[0]}
}

// A failed node keeps status failed even when its on_failure path handles it.
func (x *actionExec) settleFetch(s step, saveAs string, result fetchResult) error {
	if result.failure == "" {
		delete(x.run.Locals, localError)
		x.saveLocal(saveAs, result.value)
		x.execList(s.Branches[0])
		return nil
	}
	x.setLocal(localError, result.failure)
	first := len(x.run.Record.Actions)
	x.execList(s.Branches[1])
	x.noteHandled(x.run.Record.Actions[first:])
	return failure(result.failure)
}

func (x *actionExec) noteHandled(path []automationrpc.ActionRecord) {
	good, bad := tallyRecords(path)
	if good > 0 && bad == 0 {
		x.budget.handled++
	}
}

func tallyRecords(records []automationrpc.ActionRecord) (good, bad int) {
	for _, r := range records {
		switch r.Status {
		case automationrpc.StatusOK:
			good++
		case automationrpc.StatusFailed, automationrpc.StatusLimited, automationrpc.StatusUnsupported:
			bad++
		}
	}
	return good, bad
}
