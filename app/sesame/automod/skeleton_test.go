package automod

import (
	"testing"

	"ItsBagelBot/app/sesame/module"
)

func TestNormalizeConfusables(t *testing.T) {
	cases := map[string]string{
		"GR" + string(rune(0x0410)) + "BIFY": "grabify", // uppercase Cyrillic А (the old gap)
		"gr" + string(rune(0x03b1)) + "bify": "grabify", // Greek alpha
		"GR" + string(rune(0x0391)) + "BIFY": "grabify", // uppercase Greek Alpha
		"gr4b1fy":                            "grabify", // digit leet
		"5cam":                               "scam",    // 5 -> s
		"8ig":                                "big",     // 8 -> b
	}
	for in, want := range cases {
		if got := string(Normalize(nil, in)); got != want {
			t.Fatalf("Normalize(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestInspectUppercaseCyrillicEvasion(t *testing.T) {
	g := New()
	// "grAbify.link" with an uppercase Cyrillic А used to slip the fold (lowercased
	// to a Cyrillic а that was never folded). It must now normalize and match.
	line := "please visit gr" + string(rune(0x0410)) + "bify.link for the reward soon"
	if v := g.Inspect(module.RoleEveryone, line); v.Rule != "ip_logger" {
		t.Fatalf("uppercase Cyrillic evasion not caught: rule=%s action=%s", v.Rule, v.Action)
	}
}
