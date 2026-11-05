// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package automation

import (
	"strconv"
	"strings"
)

const (
	CompareGTE      = "gte"
	CompareLT       = "lt"
	CompareEQ       = "eq"
	CompareContains = "contains"
)

var compareTemplateOps = enum{CompareGTE, CompareLT, CompareEQ, CompareContains}

type CompareCondition struct {
	Left  string `json:"left"`
	Op    string `json:"op"`
	Right string `json:"right"`
}

func checkCompare(c chk, p CompareCondition) {
	c.template("left", p.Left)
	c.oneOf("op", p.Op, compareTemplateOps)
	c.text("right", p.Right, compareRight(p.Op))
}

func compareRight(op string) span {
	if op == CompareContains {
		return nonEmpty(MaxTemplateLength)
	}
	return upTo(MaxTemplateLength)
}

// ComparePasses must agree with comparePasses in web/kit/lib/automations/compare.ts; testdata/compare_cases.json pins both.
func ComparePasses(op, left, right string) bool {
	switch op {
	case CompareEQ:
		return left == right
	case CompareContains:
		return right != "" && strings.Contains(left, right)
	case CompareGTE, CompareLT:
		return orderedPasses(op, left, right)
	}
	return false
}

func orderedPasses(op, left, right string) bool {
	a, okLeft := decimalValue(left)
	b, okRight := decimalValue(right)
	if !okLeft || !okRight {
		return false
	}
	if op == CompareLT {
		return a < b
	}
	return a >= b
}

func decimalValue(s string) (float64, bool) {
	s = strings.Trim(s, " \t\r\n")
	if s == "" || strings.Trim(s, "0123456789+-.eE") != "" {
		return 0, false
	}
	v, err := strconv.ParseFloat(s, 64)
	return v, err == nil
}
