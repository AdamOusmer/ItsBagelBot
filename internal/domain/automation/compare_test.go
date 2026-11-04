// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package automation_test

import (
	"encoding/json"
	"fmt"
	"testing"

	"ItsBagelBot/internal/domain/automation"
)

type compareCase struct {
	Left  string `json:"left"`
	Op    string `json:"op"`
	Right string `json:"right"`
	Want  bool   `json:"want"`
}

func TestComparePassesMatchesSharedTable(t *testing.T) {
	var cases []compareCase
	if err := json.Unmarshal(readFile(t, "testdata/compare_cases.json"), &cases); err != nil {
		t.Fatal(err)
	}
	for _, tc := range cases {
		t.Run(fmt.Sprintf("%q %s %q", tc.Left, tc.Op, tc.Right), func(t *testing.T) {
			if got := automation.ComparePasses(tc.Op, tc.Left, tc.Right); got != tc.Want {
				t.Fatalf("ComparePasses = %v, want %v", got, tc.Want)
			}
		})
	}
}
