// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"strings"
	"testing"

	"ItsBagelBot/internal/domain/automation"

	"github.com/stretchr/testify/assert"
)

func TestTextStepsOnHostileInput(t *testing.T) {
	huge := strings.Repeat("é", 200_000)
	args := map[string]string{"huge": huge, "n": "1"}
	cases := []struct {
		name   string
		params obj
		status string
		want   string
	}{
		{"upper", obj{"op": "upper", "input": "aé"}, "ok", "AÉ"},
		{"length counts runes", obj{"op": "length", "input": "{arg:huge}"}, "ok", "200000"},
		{"huge replace", obj{"op": "replace", "input": "{arg:huge}", "arg1": "é", "arg2": "{arg:huge}"}, "ok", strings.Repeat("é", automation.MaxSavedValueRunes)},
		{"empty separator splits runes", obj{"op": "split_pick", "input": "aéb", "arg1": "", "arg2": "2"}, "ok", "é"},
		{"split index past the end", obj{"op": "split_pick", "input": "a,b", "arg1": ",", "arg2": "9"}, "ok", ""},
		{"split index zero", obj{"op": "split_pick", "input": "a,b", "arg1": ",", "arg2": "0"}, "ok", ""},
		{"split bad index", obj{"op": "split_pick", "input": "a,b", "arg1": ",", "arg2": "x"}, "failed", ""},
		{"substring past the end", obj{"op": "substring", "input": "abc", "arg1": "99", "arg2": "1"}, "ok", ""},
		{"substring negative start", obj{"op": "substring", "input": "abc", "arg1": "-5", "arg2": "2"}, "ok", "ab"},
		{"substring huge length", obj{"op": "substring", "input": "abc", "arg1": "1", "arg2": "9223372036854775807"}, "ok", "bc"},
		{"substring in runes", obj{"op": "substring", "input": "éàb", "arg1": "1", "arg2": "1"}, "ok", "à"},
		{"substring bad start", obj{"op": "substring", "input": "abc", "arg1": "x", "arg2": "1"}, "failed", ""},
		{"substring overflowing start", obj{"op": "substring", "input": "abc", "arg1": "99999999999999999999", "arg2": "1"}, "failed", ""},
		{"contains", obj{"op": "contains", "input": "abc", "arg1": "b"}, "ok", "true"},
		{"url encode", obj{"op": "url_encode", "input": "a b&c"}, "ok", "a+b%26c"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tc.params["save_as"] = "out"
			fx := newAutoFixture(manual("main", act("text", tc.params), setVar("out", "{local:out}")))
			rec := fx.run(t, "main", "req-text", args)
			assert.Equal(t, tc.status, rec.Actions[0].Status, statuses(rec))
			assert.Equal(t, tc.want, fx.vars.value(channelVars, "out"))
		})
	}
}

func TestMathStepsOnHostileInput(t *testing.T) {
	deep := strings.Repeat("(", 98) + "1" + strings.Repeat(")", 98)
	cases := []struct {
		name string
		expr string
		want string
	}{
		{"precedence", "1 + 2 * 3 ^ 2", "19"},
		{"parentheses", "(1 + 2) * 3", "9"},
		{"unary", "-2 ^ 2", "-4"},
		{"right associative power", "2 ^ 3 ^ 2", "512"},
		{"modulo", "10 % 3", "1"},
		{"decimal", "1 / 4", "0.25"},
		{"integer has no point", "10 / 2", "5"},
		{"negative zero", "-0", "0"},
		{"functions", "max(round(1.5), floor(2.9), ceil(0.1), abs(-1), min(9, 8))", "8"},
		{"token first", "{arg:n} * 2", "42"},
		{"random lower bound with fixed roll", "random(5, 9)", "5"},
		{"random swaps bounds", "random(9, 5)", "5"},
		{"deep but in limit", deep, "1"},
		{"float64 precision is the contract", "2 ^ 64", "18446744073709552000"},
		{"unknown function", "evil(1)", ""},
		{"divide by zero", "1 / 0", ""},
		{"modulo by zero", "1 % 0", ""},
		{"parse error", "1 +", ""},
		{"trailing text", "1 2", ""},
		{"empty", "{arg:absent}", ""},
		{"overflow", "9 ^ 9 ^ 9 ^ 9", ""},
		{"overflow by product", "10 ^ 308 * 10", ""},
		{"nan", "(0 - 1) ^ 0.5", ""},
		{"random range overflow", "random(-1 * 10 ^ 308, 10 ^ 308)", ""},
		{"too many parentheses", strings.Repeat("(", 150) + "1" + strings.Repeat(")", 150), ""},
		{"unbalanced parentheses", strings.Repeat("(", 150), ""},
		{"wrong argument count", "abs(1, 2)", ""},
		{"no arguments", "max()", ""},
		{"code", "os.exit(1)", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fx := newAutoFixture(manual("main", act("math", obj{"expression": tc.expr, "save_as": "out"}), setVar("out", "[{local:out}]")))
			fx.p.autoIntN = func(int) int { return 0 }
			rec := fx.run(t, "main", "req-math", map[string]string{"n": "21"})
			if tc.want == "" {
				assert.Equal(t, "failed", rec.Actions[0].Status, statuses(rec))
				assert.Equal(t, "math_error", rec.Actions[0].Detail)
				assert.Equal(t, "[]", fx.vars.value(channelVars, "out"), "failure saves nothing")
				return
			}
			assert.Equal(t, "ok", rec.Actions[0].Status, statuses(rec))
			assert.Equal(t, "["+tc.want+"]", fx.vars.value(channelVars, "out"))
		})
	}
}
