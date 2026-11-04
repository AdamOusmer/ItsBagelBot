// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package automation_test

import (
	"regexp"
	"testing"

	"ItsBagelBot/pkg/codec"
)

type regexCase struct {
	Pattern string `json:"pattern"`
	Want    bool   `json:"want"`
}

func TestRegexTableMatchesGoCompiler(t *testing.T) {
	var cases []regexCase
	if err := codec.Unmarshal(readFile(t, "testdata/regex_cases.json"), &cases); err != nil {
		t.Fatal(err)
	}
	for _, tc := range cases {
		t.Run(tc.Pattern, func(t *testing.T) {
			if _, err := regexp.Compile(tc.Pattern); (err == nil) != tc.Want {
				t.Fatalf("regexp.Compile err = %v, want compiles = %v", err, tc.Want)
			}
		})
	}
}
