// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package live

import (
	"regexp"
	"strconv"
	"testing"
)

// placeholderPattern matches the KEYS[n] / ARGV[n] slots a script reads.
var placeholderPattern = regexp.MustCompile(`(KEYS|ARGV)\[(\d+)\]`)

// scriptArity returns the highest KEYS and ARGV index a script reads, which is
// the number of keys and arguments its callers must pass. Valkey does not
// bounds-check: an out-of-range index yields nil, and the failure only shows up
// later as a type error deep inside the script (tonumber(nil), then "Command
// arguments must be strings or integers"). Scanning the source is the only
// offline way to catch that without a live server.
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

// TestScriptArityMatchesCallers pins each script's arity to what its callers
// actually pass. Set takes a ver TTL as its third argument; Clear deletes the
// live key rather than writing it, so it has no live-key TTL and takes two.
// #561 regressed exactly this by copying Set's last line into Clear, which read
// ARGV[3] against two arguments and made every offline write fail in
// production. Callers: outgress LiveWriter.Write
// (app/twitch/outgress/internal/worker/live.go) and sesame setLiveKey /
// clearLiveKey (app/twitch/sesame/engine/live_valkey.go). Changing a want here
// means those call sites must change with it.
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
