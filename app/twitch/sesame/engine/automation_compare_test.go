// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"ItsBagelBot/internal/domain/automation"
	automationrpc "ItsBagelBot/internal/domain/rpc/automation"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const compareCasesPath = "../../../../internal/domain/automation/testdata/compare_cases.json"

type compareCase struct {
	Left  string `json:"left"`
	Op    string `json:"op"`
	Right string `json:"right"`
	Want  bool   `json:"want"`
}

func loadCompareCases(t *testing.T) []compareCase {
	t.Helper()
	raw, err := os.ReadFile(compareCasesPath)
	require.NoError(t, err)
	var cases []compareCase
	require.NoError(t, json.Unmarshal(raw, &cases))
	return cases
}

func compareCondition(left, op, right string) automation.Condition {
	raw, err := json.Marshal(obj{"left": left, "op": op, "right": right})
	if err != nil {
		panic(err)
	}
	return automation.Condition{Type: "compare", Params: raw}
}

func compareIf(left, op, right string) automation.Action {
	return act("if", obj{
		"condition": obj{"type": "compare", "params": obj{"left": left, "op": op, "right": right}},
		"then":      []automation.Action{act("chat", obj{"text": "yes"})},
		"else":      []automation.Action{act("chat", obj{"text": "no"})},
	})
}

func TestAutomationCompareInsideIfMatchesSharedTable(t *testing.T) {
	for _, tc := range loadCompareCases(t) {
		t.Run(fmt.Sprintf("%q %s %q", tc.Left, tc.Op, tc.Right), func(t *testing.T) {
			fx := newAutoFixture(manual("main", compareIf(tc.Left, tc.Op, tc.Right)))
			rec := fx.run(t, "main", "req-compare", nil)
			want := "no"
			if tc.Want {
				want = "yes"
			}
			assert.Equal(t, []string{`chat {"broadcaster_id":"1","message":"` + want + `"}`}, fx.described(t))
			assert.Equal(t, automationrpc.OutcomeOK, rec.Outcome)
		})
	}
}

func TestAutomationCompareResolvesTemplatesAtRunTime(t *testing.T) {
	auto := manual("main", act("chat", obj{"text": "big"}))
	auto.Definition.Conditions = []automation.Condition{compareCondition("{arg:n}", "gte", "{arg:floor}")}
	cases := []struct {
		name    string
		args    map[string]string
		passes  bool
		outcome string
	}{
		{"above", map[string]string{"n": "42", "floor": "10"}, true, automationrpc.OutcomeOK},
		{"equal", map[string]string{"n": "10", "floor": "10"}, true, automationrpc.OutcomeOK},
		{"below", map[string]string{"n": "5", "floor": "10"}, false, automationrpc.OutcomeSkipped},
		{"not a number", map[string]string{"n": "lots", "floor": "10"}, false, automationrpc.OutcomeSkipped},
		{"missing", map[string]string{"floor": "10"}, false, automationrpc.OutcomeSkipped},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fx := newAutoFixture(auto)
			rec := fx.run(t, "main", "req-"+tc.name, tc.args)
			assert.Equal(t, tc.outcome, rec.Outcome)
			assert.Equal(t, tc.passes, len(fx.messages(t)) == 1)
			if !tc.passes {
				assert.Equal(t, "condition:0:compare", rec.Reason)
			}
		})
	}
}
