// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package live

import (
	"regexp"
	"strconv"
	"testing"
)

var placeholderPattern = regexp.MustCompile(`(KEYS|ARGV)\[(\d+)\]`)

func scriptArity(script string) (keys, args int) {
	for _, m := range placeholderPattern.FindAllStringSubmatch(script, -1) {
		n, _ := strconv.Atoi(m[2])
		if m[1] == "KEYS" {
			keys = max(keys, n)
			continue
		}
		args = max(args, n)
	}
	return keys, args
}

func TestScriptArityMatchesCallers(t *testing.T) {
	tests := []struct {
		name     string
		script   string
		wantKeys int
		wantArgs int
	}{
		{name: "SetScript", script: SetScript, wantKeys: 2, wantArgs: 3},
		{name: "ClearScript", script: ClearScript, wantKeys: 2, wantArgs: 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			keys, args := scriptArity(tt.script)
			if keys != tt.wantKeys {
				t.Errorf("%s reads %d keys, callers pass %d", tt.name, keys, tt.wantKeys)
			}
			if args != tt.wantArgs {
				t.Errorf("%s reads %d args, callers pass %d", tt.name, args, tt.wantArgs)
			}
		})
	}
}
