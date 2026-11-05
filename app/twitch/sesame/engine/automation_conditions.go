// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"errors"

	"ItsBagelBot/internal/domain/automation"
)

const conditionCooldown = "cooldown"

var errConditionParams = errors.New("condition params type mismatch")

type conditionCheck func(x *actionExec, c *automation.PlannedCondition) (bool, error)

func checkParams[T any](fn func(*actionExec, T) (bool, error)) conditionCheck {
	return func(x *actionExec, c *automation.PlannedCondition) (bool, error) {
		if c.Err != nil {
			return false, c.Err
		}
		params, ok := c.Params.(T)
		if !ok {
			return false, errConditionParams
		}
		return fn(x, params)
	}
}

func checkWith[T any](fn func(*Pipeline, context.Context, *AutomationRun, T) (bool, error)) conditionCheck {
	return checkParams(func(x *actionExec, params T) (bool, error) {
		return fn(x.p, x.ctx, x.run, params)
	})
}

var conditionChecks = map[string]conditionCheck{
	"perm":            checkWith((*Pipeline).conditionPerm),
	"users":           checkWith((*Pipeline).conditionUsers),
	"live":            checkWith((*Pipeline).conditionLive),
	conditionCooldown: checkWith((*Pipeline).conditionCooldown),
	"chance":          checkWith((*Pipeline).conditionChance),
	"counter":         checkWith((*Pipeline).conditionCounter),
	"points":          checkWith((*Pipeline).conditionPoints),
	"arg_regex":       checkWith((*Pipeline).conditionArgRegex),
	"category":        checkWith((*Pipeline).conditionCategory),
	"compare":         checkParams((*actionExec).conditionCompare),
	"var":             checkParams((*actionExec).conditionVar),
}

func (x *actionExec) conditionsPass() (bool, string, error) {
	conds := x.run.Plan.Conditions
	for i := range conds {
		pass, err := x.condition(&conds[i])
		if err != nil || !pass {
			return false, conds[i].Reason, err
		}
	}
	return true, "", nil
}

func (x *actionExec) condition(cond *automation.PlannedCondition) (bool, error) {
	check := conditionChecks[cond.Type]
	if check == nil {
		return false, nil
	}
	return check(x, cond)
}

func (x *actionExec) conditionCompare(c automation.CompareCondition) (bool, error) {
	return automation.ComparePasses(c.Op, x.expand(c.Left), x.expand(c.Right)), nil
}
