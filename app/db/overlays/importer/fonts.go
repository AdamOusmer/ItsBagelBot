// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package importer

import "strings"

const fallbackFont = "Inter"

var bundledFonts = []string{"Inter", "Roboto", "Poppins", "Montserrat", "Oswald", "Lora", "Roboto Mono"}

var fontHints = []struct{ hint, font string }{
	{"sans", "Inter"},
	{"mono", "Roboto Mono"},
	{"code", "Roboto Mono"},
	{"serif", "Lora"},
	{"slab", "Lora"},
	{"times", "Lora"},
	{"georgia", "Lora"},
	{"condensed", "Oswald"},
	{"narrow", "Oswald"},
	{"impact", "Oswald"},
}

func nearestFont(family string) (string, bool) {
	for _, font := range bundledFonts {
		if strings.EqualFold(font, family) {
			return font, true
		}
	}
	lower := strings.ToLower(family)
	for _, hint := range fontHints {
		if strings.Contains(lower, hint.hint) {
			return hint.font, false
		}
	}
	return fallbackFont, false
}
