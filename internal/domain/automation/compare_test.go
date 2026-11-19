// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package automation_test

import (
	"fmt"
	"testing"

	"ItsBagelBot/internal/domain/automation"
	"ItsBagelBot/pkg/codec"
)

type compareCase struct {
	automation.CompareCondition
	Want bool `json:"want"`
}

func TestComparePassesMatchesSharedTable(t *testing.T) {
	var cases []compareCase
	if err := codec.Unmarshal(readFile(t, "testdata/compare_cases.json"), &cases); err != nil {
		t.Fatal(err)
	}
	for _, tc := range cases {
		t.Run(fmt.Sprintf("%q %s %q ignore_case=%t", tc.Left, tc.Op, tc.Right, tc.IgnoreCase), func(t *testing.T) {
			if got := automation.ComparePasses(tc.Op, tc.Left, tc.Right, tc.IgnoreCase); got != tc.Want {
				t.Fatalf("ComparePasses = %v, want %v", got, tc.Want)
			}
		})
	}
}
