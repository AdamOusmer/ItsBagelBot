// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package automation_test

import (
	"math"
	"strconv"
	"strings"
	"testing"

	"ItsBagelBot/internal/domain/automation"

	"github.com/stretchr/testify/assert"
)

func TestAddToVariable(t *testing.T) {
	cases := []struct {
		current string
		amount  int64
		want    string
		ok      bool
	}{
		{"", 2, "2", true},
		{"5", -7, "-2", true},
		{" 5 ", 1, "6", true},
		{"banana", 3, "3", true},
		{"1.5", 2, "3.5", true},
		{"{arg:1}", 1, "1", true},
		{strconv.FormatInt(math.MaxInt64, 10), 1, "", false},
		{strconv.FormatInt(math.MinInt64, 10), -1, "", false},
	}
	for _, tc := range cases {
		got, ok := automation.VariableValue(tc.current).Add(tc.amount)
		assert.Equal(t, tc.ok, ok, tc.current)
		assert.Equal(t, tc.want, got, tc.current)
	}
}

func TestVariableNameIsCaseInsensitive(t *testing.T) {
	name, ok := automation.VariableName("Deaths_2")
	assert.True(t, ok)
	assert.Equal(t, "deaths_2", name)
	for _, bad := range []string{"", "has space", "dash-ed", strings.Repeat("a", 33), "é"} {
		_, ok := automation.VariableName(bad)
		assert.False(t, ok, bad)
	}
}

func TestEvalMathNeverPanicsOnArbitraryText(t *testing.T) {
	inputs := []string{
		"", "(", ")", "()", "(()", ",", "max(", "max(,", "max(1,", "random(1)", "random()", "^", "-", "--", "1^", "..", "1.2.3", ".",
		strings.Repeat("-", 200), strings.Repeat("(", 200), strings.Repeat("max(", 50), strings.Repeat("1^", 99) + "1", "é", "\x00", "1\n+1",
		strings.Repeat("é", 201), strings.Repeat("9", 201),
	}
	for _, input := range inputs {
		assert.NotPanics(t, func() { _, _ = automation.EvalMath(input, func() float64 { return 0.5 }) }, input)
	}
	_, err := automation.EvalMath(strings.Repeat("1", automation.MaxMathExpressionRunes+1), func() float64 { return 0 })
	assert.ErrorIs(t, err, automation.ErrMath)
}

func TestApplyTextNeverPanicsOnArbitraryInput(t *testing.T) {
	ops := []string{"upper", "lower", "trim", "length", "replace", "substring", "split_pick", "contains", "url_encode", "unknown"}
	values := []string{"", " ", "\xff\xfe", "é", "-1", "0", "9223372036854775807", "-9223372036854775808", "99999999999999999999", "random", "RANDOM", strings.Repeat("é", 5000)}
	for _, op := range ops {
		for _, input := range values {
			for _, arg := range values {
				assert.NotPanics(t, func() {
					_, _ = automation.ApplyText(op, automation.TextOperands{Input: input, Arg1: arg, Arg2: arg}, func(n int) int { return n - 1 })
				}, op)
			}
		}
	}
}

func TestTruncateRunes(t *testing.T) {
	assert.Equal(t, "éé", automation.TruncateRunes("ééé", 2))
	assert.Equal(t, "abc", automation.TruncateRunes("abc", 3))
	assert.Equal(t, "", automation.TruncateRunes("abc", 0))
}
