// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package automation

import (
	"cmp"
	"math"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

const (
	VariableScopeChannel = "channel"
	VariableScopeViewer  = "viewer"

	varOpExists = "exists"
)

var (
	variableScopes      = enum{VariableScopeChannel, VariableScopeViewer}
	varConditionOps     = enum{"eq", "ne", "gt", "gte", "lt", "lte", CompareContains, varOpExists}
	variableNamePattern = regexp.MustCompile(`^[a-z0-9_]{1,32}$`)
)

type VarSetAction struct {
	Scope string `json:"scope"`
	Name  string `json:"name"`
	Value string `json:"value"`
}

type VarAddAction struct {
	Scope  string `json:"scope"`
	Name   string `json:"name"`
	Amount int64  `json:"amount"`
}

type VarDeleteAction struct {
	Scope string `json:"scope"`
	Name  string `json:"name"`
}

type VarCondition struct {
	Scope string `json:"scope"`
	Name  string `json:"name"`
	Op    string `json:"op"`
	Value string `json:"value"`
}

// VariableName lowercases a variable or run local name and reports whether it is well formed.
func VariableName(raw string) (string, bool) {
	name := strings.ToLower(raw)
	return name, variableNamePattern.MatchString(name)
}

func (c chk) variableName(f field, raw string) {
	if raw == "" {
		c.add(f, CodeRequired)
		return
	}
	if _, ok := VariableName(raw); !ok {
		c.add(f, CodeInvalidFormat)
	}
}

func (c chk) optionalVariableName(f field, raw string) {
	if raw != "" {
		c.variableName(f, raw)
	}
}

func checkVarSet(c chk, p VarSetAction) {
	c.oneOf("scope", p.Scope, variableScopes)
	c.variableName("name", p.Name)
	c.text("value", p.Value, upTo(MaxTemplateLength))
}

func checkVarAdd(c chk, p VarAddAction) {
	c.oneOf("scope", p.Scope, variableScopes)
	c.variableName("name", p.Name)
	inRange(c, "amount", p.Amount, between(-MaxVariableAmount, MaxVariableAmount))
	nonZero(c, "amount", p.Amount)
}

func checkVarDelete(c chk, p VarDeleteAction) {
	c.oneOf("scope", p.Scope, variableScopes)
	c.variableName("name", p.Name)
}

func checkVarCondition(c chk, p VarCondition) {
	c.oneOf("scope", p.Scope, variableScopes)
	c.variableName("name", p.Name)
	c.oneOf("op", p.Op, varConditionOps)
	c.text("value", p.Value, upTo(MaxTemplateLength))
}

// VarPasses compares numerically when both sides parse as numbers, otherwise as text.
func VarPasses(op, current, value string, exists bool) bool {
	switch op {
	case varOpExists:
		return exists
	case CompareContains:
		return value != "" && strings.Contains(current, value)
	}
	return orderPasses(op, compareVarValues(current, value))
}

func compareVarValues(left, right string) int {
	a, okLeft := decimalValue(left)
	b, okRight := decimalValue(right)
	if okLeft && okRight {
		return cmp.Compare(a, b)
	}
	return strings.Compare(left, right)
}

func orderPasses(op string, order int) bool {
	switch op {
	case "eq":
		return order == 0
	case "ne":
		return order != 0
	case "gt":
		return order > 0
	case "gte":
		return order >= 0
	case "lt":
		return order < 0
	case "lte":
		return order <= 0
	}
	return false
}

// AddToVariable applies var_add to a stored value; anything non-numeric counts as 0. It reports false on overflow.
func AddToVariable(current string, amount int64) (string, bool) {
	trimmed := strings.TrimSpace(current)
	if base, err := strconv.ParseInt(trimmed, 10, 64); err == nil {
		return addIntegers(base, amount)
	}
	base, ok := decimalValue(trimmed)
	if !ok {
		return addIntegers(0, amount)
	}
	return strconv.FormatFloat(base+float64(amount), 'f', -1, 64), true
}

func addIntegers(base, amount int64) (string, bool) {
	overflow := amount > 0 && base > math.MaxInt64-amount
	underflow := amount < 0 && base < math.MinInt64-amount
	if overflow || underflow {
		return "", false
	}
	return strconv.FormatInt(base+amount, 10), true
}

// TruncateRunes keeps at most limit runes of s.
func TruncateRunes(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	count := 0
	for i := range s {
		if count == limit {
			return s[:i]
		}
		count++
	}
	return s
}

// ValidVariableValue reports whether a rendered value fits in a stored variable.
func ValidVariableValue(value string) bool {
	return utf8.RuneCountInString(value) <= MaxVariableValueRunes
}
