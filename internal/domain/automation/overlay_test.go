// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package automation_test

import (
	"path/filepath"
	"strings"
	"testing"

	"ItsBagelBot/internal/domain/automation"
)

func TestOverlayMediaURL(t *testing.T) {
	cases := map[string]bool{
		"https://cdn.example.com/a.png":  true,
		"HTTPS://cdn.example.com/a.png":  true,
		"http://cdn.example.com/a.png":   false,
		"javascript:alert(1)":            false,
		"data:image/png;base64,AAAA":     false,
		"https://":                       false,
		"https:cdn.example.com":          false,
		`https:\\cdn.example.com`:        false,
		"https://user:pw@cdn.example":    false,
		"//cdn.example.com/a.png":        false,
		" https://cdn.example.com/a.png": false,
		"":                               false,
		"https://x.example/" + strings.Repeat("a", automation.MaxOverlayURLLength): false,
	}
	for raw, want := range cases {
		if got := automation.OverlayMediaURL(raw); got != want {
			t.Errorf("OverlayMediaURL(%q) = %v, want %v", raw, got, want)
		}
	}
}

func TestOverlayWireMatchesTheDashboardGolden(t *testing.T) {
	zero := 0
	items := []automation.OverlayItem{
		{Version: 1, Kind: "text", Content: "</script><img src=x onerror=alert(1)>", Seconds: 5, Position: "top"},
		{Version: 1, Kind: "image", Content: "https://cdn.example.com/a.png", Seconds: 60, Position: "center"},
		{Version: 1, Kind: "sound", Content: "https://cdn.example.com/horn.mp3", Volume: &zero},
	}
	checkGolden(t, filepath.Join("testdata", "overlay_wire.golden.json"), indented(t, items))
}
