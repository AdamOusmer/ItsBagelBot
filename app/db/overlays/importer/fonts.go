// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package importer

import (
	"slices"
	"strings"
)

const fallbackFont = "Inter"

var bundledFonts = []string{"Inter", "Roboto", "Poppins", "Montserrat", "Oswald", "Lora", "Roboto Mono"}

type fontHint struct {
	hint string
	font string
}

var fontHints = []fontHint{
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
	exact := slices.IndexFunc(bundledFonts, func(font string) bool { return strings.EqualFold(font, family) })
	if exact >= 0 {
		return bundledFonts[exact], true
	}
	lower := strings.ToLower(family)
	hinted := slices.IndexFunc(fontHints, func(h fontHint) bool { return strings.Contains(lower, h.hint) })
	if hinted >= 0 {
		return fontHints[hinted].font, false
	}
	return fallbackFont, false
}
