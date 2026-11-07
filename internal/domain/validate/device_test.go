// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package validate_test

import (
	"testing"

	"ItsBagelBot/internal/domain/validate"
)

func TestDeviceNameStripsBidiAndFormatCharacters(t *testing.T) {
	rlo, pdf := string(rune(0x202E)), string(rune(0x202C))
	lri, pdi := string(rune(0x2066)), string(rune(0x2069))
	zwsp := string(rune(0x200B))

	cases := map[string]string{
		"Mac" + rlo + "evil" + pdf:   "Macevil",
		lri + "Studio" + pdi + " PC": "Studio PC",
		"Zero" + zwsp + "width":      "Zerowidth",
		"Line\nbreak":                "Linebreak",
		"Adam's MacBook":             "Adam's MacBook",
		"Café 配信":                    "Café 配信",
	}
	for raw, want := range cases {
		if got := validate.DeviceName(raw); got != want {
			t.Errorf("DeviceName(%q) = %q, want %q", raw, got, want)
		}
	}
}
