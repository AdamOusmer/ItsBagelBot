// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package automation

import (
	"cmp"
	"strconv"
	"strings"
)

const (
	CompareEQ       = "eq"
	CompareNE       = "ne"
	CompareGT       = "gt"
	CompareGTE      = "gte"
	CompareLT       = "lt"
	CompareLTE      = "lte"
	CompareContains = "contains"
	CompareStarts   = "starts"
)

var compareTemplateOps = enum{CompareEQ, CompareNE, CompareGT, CompareGTE, CompareLT, CompareLTE, CompareContains, CompareStarts}

type CompareCondition struct {
	Left       string `json:"left"`
	Op         string `json:"op"`
	Right      string `json:"right"`
	IgnoreCase bool   `json:"ignore_case,omitempty"`
}

func checkCompare(c chk, p CompareCondition) {
	c.template("left", p.Left)
	c.oneOf("op", p.Op, compareTemplateOps)
	c.text("right", p.Right, compareRight(p.Op))
}

func compareRight(op string) span {
	if op == CompareContains || op == CompareStarts {
		return nonEmpty(MaxTemplateLength)
	}
	return upTo(MaxTemplateLength)
}

// ComparePasses must agree with comparePasses in web/kit/lib/automations/compare.ts; testdata/compare_cases.json pins both.
func ComparePasses(op, left, right string, ignoreCase bool) bool {
	if ignoreCase {
		left, right = strings.ToLower(left), strings.ToLower(right)
	}
	switch op {
	case CompareEQ:
		return left == right
	case CompareNE:
		return left != right
	case CompareContains:
		return right != "" && strings.Contains(left, right)
	case CompareStarts:
		return right != "" && strings.HasPrefix(left, right)
	}
	return orderedPasses(orderOp(op), left, right)
}

func orderedPasses(op orderOp, left, right string) bool {
	a, okLeft := decimalValue(left)
	b, okRight := decimalValue(right)
	if !okLeft || !okRight {
		return false
	}
	return op.holds(cmp.Compare(a, b))
}

func decimalValue(s string) (float64, bool) {
	s = strings.Trim(s, " \t\r\n")
	if s == "" || strings.Trim(s, "0123456789+-.eE") != "" {
		return 0, false
	}
	v, err := strconv.ParseFloat(s, 64)
	return v, err == nil
}
