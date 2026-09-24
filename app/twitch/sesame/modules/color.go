// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"strconv"
	"strings"
)

var namedColors = map[string]int{
	"red":     0xFF0000,
	"orange":  0xFF6A00,
	"yellow":  0xFFD000,
	"green":   0x00C000,
	"lime":    0x7FFF00,
	"teal":    0x008080,
	"cyan":    0x00FFFF,
	"aqua":    0x00FFFF,
	"blue":    0x0066FF,
	"navy":    0x001F7F,
	"purple":  0x8000FF,
	"violet":  0x8A2BE2,
	"indigo":  0x4B0082,
	"pink":    0xFF3FA4,
	"magenta": 0xFF00FF,
	"white":   0xFFFFFF,
	"warm":    0xFFB46B,
	"gold":    0xFFAA00,
}

func parseColor(input string) (rgb int, ok bool) {
	s := strings.ToLower(strings.TrimSpace(input))
	if s == "" {
		return 0, false
	}
	if v, found := namedColors[s]; found {
		return v, true
	}
	return parseHexColor(strings.TrimPrefix(s, "#"))
}

func parseHexColor(hex string) (int, bool) {
	switch len(hex) {
	case 6:
		return parseHex6(hex)
	case 3:
		return parseHex3(hex)
	default:
		return 0, false
	}
}

func parseHex6(hex string) (int, bool) {
	v, err := strconv.ParseInt(hex, 16, 32)
	if err != nil {
		return 0, false
	}
	return int(v), true
}

func parseHex3(hex string) (int, bool) {
	var v int
	for i := 0; i < len(hex); i++ {
		d, ok := hexNibble(hex[i])
		if !ok {
			return 0, false
		}
		v = v<<8 | d<<4 | d
	}
	return v, true
}

func hexNibble(c byte) (int, bool) {
	switch {
	case c >= '0' && c <= '9':
		return int(c - '0'), true
	case c >= 'a' && c <= 'f':
		return int(c-'a') + 10, true
	case c >= 'A' && c <= 'F':
		return int(c-'A') + 10, true
	}
	return 0, false
}

func colorNames() []string {
	return []string{
		"red", "orange", "yellow", "green", "lime", "teal", "cyan",
		"blue", "navy", "purple", "violet", "indigo", "pink", "magenta",
		"white", "warm", "gold",
	}
}
