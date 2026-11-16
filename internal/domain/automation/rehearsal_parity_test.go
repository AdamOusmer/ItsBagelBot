// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package automation_test

import (
	"testing"

	"ItsBagelBot/internal/domain/automation"
	"ItsBagelBot/pkg/codec"
)

type rehearsalCase struct {
	Expression string `json:"expression"`
	Op         string `json:"op"`
	Input      string `json:"input"`
	Arg1       string `json:"arg1"`
	Arg2       string `json:"arg2"`
	Want       string `json:"want"`
	Error      string `json:"error"`
}

func TestRehearsalSharedOperations(t *testing.T) {
	for _, file := range []string{"math_cases.json", "text_cases.json"} {
		t.Run(file, func(t *testing.T) { checkRehearsalFile(t, file) })
	}
}

func checkRehearsalFile(t *testing.T, file string) {
	t.Helper()
	var cases []rehearsalCase
	if err := codec.Unmarshal(readFile(t, "../../../web/kit/lib/automations/testdata/"+file), &cases); err != nil {
		t.Fatal(err)
	}
	for _, tc := range cases {
		t.Run(tc.Expression+tc.Op+tc.Input+tc.Arg2, func(t *testing.T) { checkRehearsalCase(t, tc) })
	}
}

func checkRehearsalCase(t *testing.T, tc rehearsalCase) {
	t.Helper()
	got, err := runRehearsalCase(tc)
	code := ""
	if err != nil {
		code = err.Error()
	}
	if got != tc.Want || code != tc.Error {
		t.Fatalf("got (%q, %q), want (%q, %q)", got, code, tc.Want, tc.Error)
	}
}

func runRehearsalCase(tc rehearsalCase) (string, error) {
	if tc.Op == "" {
		return automation.EvalMath(tc.Expression, func() float64 { return 0 })
	}
	return automation.ApplyText(tc.Op, automation.TextOperands{Input: tc.Input, Arg1: tc.Arg1, Arg2: tc.Arg2}, func(int) int { return 0 })
}
