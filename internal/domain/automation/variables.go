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
	variableNamePattern = regexp.MustCompile(`^[a-z0-9]{1,32}$`)
)

// VarRef names one stored variable. Embedded untagged, so its fields stay flat in the JSON.
type VarRef struct {
	Scope string `json:"scope"`
	Name  string `json:"name"`
}

type VarSetAction struct {
	VarRef
	Value string `json:"value"`
}

type VarAddAction struct {
	VarRef
	Amount int64 `json:"amount"`
}

type VarDeleteAction struct {
	VarRef
}

type VarCondition struct {
	VarRef
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
		c.add(f, CodeSimpleWord)
	}
}

func (c chk) optionalVariableName(f field, raw string) {
	if raw != "" {
		c.variableName(f, raw)
	}
}

func (c chk) varRef(r VarRef) {
	c.oneOf("scope", r.Scope, variableScopes)
	c.variableName("name", r.Name)
}

func checkVarSet(c chk, p VarSetAction) {
	c.varRef(p.VarRef)
	c.text("value", p.Value, upTo(MaxTemplateLength))
}

func checkVarAdd(c chk, p VarAddAction) {
	c.varRef(p.VarRef)
	inRange(c, "amount", p.Amount, between(-MaxVariableAmount, MaxVariableAmount))
	nonZero(c, "amount", p.Amount)
}

func checkVarDelete(c chk, p VarDeleteAction) {
	c.varRef(p.VarRef)
}

func checkVarCondition(c chk, p VarCondition) {
	c.varRef(p.VarRef)
	c.oneOf("op", p.Op, varConditionOps)
	c.text("value", p.Value, upTo(MaxTemplateLength))
}

// VariableValue is the text a variable stores.
type VariableValue string

// VarComparison is one var condition with its stored value resolved.
type VarComparison struct {
	Op      string
	Current VariableValue
	Value   VariableValue
	Exists  bool
}

// Passes compares numerically when both sides parse as numbers, otherwise as text.
func (v VarComparison) Passes() bool {
	switch v.Op {
	case varOpExists:
		return v.Exists
	case CompareContains:
		return v.Value != "" && strings.Contains(string(v.Current), string(v.Value))
	}
	return varOrderOp(v.Op).holds(v.Current.compare(v.Value))
}

func (v VariableValue) compare(other VariableValue) int {
	a, okLeft := decimalValue(string(v))
	b, okRight := decimalValue(string(other))
	if okLeft && okRight {
		return cmp.Compare(a, b)
	}
	return strings.Compare(string(v), string(other))
}

type varOrderOp string

func (op varOrderOp) holds(order int) bool {
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

// Add applies var_add to a stored value; anything non-numeric counts as 0. It reports false on overflow.
func (v VariableValue) Add(amount int64) (string, bool) {
	trimmed := strings.TrimSpace(string(v))
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

// Fits reports whether a rendered value fits in a stored variable.
func (v VariableValue) Fits() bool {
	return utf8.RuneCountInString(string(v)) <= MaxVariableValueRunes
}
