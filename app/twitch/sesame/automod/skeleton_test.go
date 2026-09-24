// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package automod

import (
	"testing"

	"ItsBagelBot/app/twitch/sesame/module"
)

func TestNormalizeConfusables(t *testing.T) {
	cases := map[string]string{
		"GR" + string(rune(0x0410)) + "BIFY": "grabify",
		"gr" + string(rune(0x03b1)) + "bify": "grabify",
		"GR" + string(rune(0x0391)) + "BIFY": "grabify",
		"gr4b1fy":                            "grabify",
		"5cam":                               "scam",
		"8ig":                                "big",
	}
	for in, want := range cases {
		if got := string(Normalize(nil, in)); got != want {
			t.Fatalf("Normalize(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestInspectUppercaseCyrillicEvasion(t *testing.T) {
	g := New()
	line := "please visit gr" + string(rune(0x0410)) + "bify.link for the reward soon"
	if v := g.Inspect(module.RoleEveryone, line); v.Rule != "ip_logger" {
		t.Fatalf("uppercase Cyrillic evasion not caught: rule=%s action=%s", v.Rule, v.Action)
	}
}
