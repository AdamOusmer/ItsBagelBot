package modules

import (
	"strconv"
	"strings"
)

// namedColors maps the colour words a viewer can type in a redemption to their
// packed 24-bit RGB value. It is a deliberately small, unambiguous set: common
// colour names people actually say in chat, no shades that only differ by a few
// bits. Anything not here must be given as a hex code.
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

// parseColor turns a viewer's colour input into a packed 24-bit RGB value. It
// accepts a named colour (case-insensitive), a "#rrggbb"/"rrggbb" hex code, or
// the "#rgb"/"rgb" short form (each nibble doubled). ok is false when the input
// is empty or matches nothing, so the caller can refund the redemption and tell
// the viewer instead of silently setting a wrong colour.
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

// parseHexColor reads a 6- or 3-digit hex colour (no leading '#'). The 3-digit
// form doubles each nibble ("f80" -> "ff8800"), matching CSS.
func parseHexColor(hex string) (int, bool) {
	switch len(hex) {
	case 6:
		v, err := strconv.ParseInt(hex, 16, 32)
		if err != nil {
			return 0, false
		}
		return int(v), true
	case 3:
		var v int
		for _, r := range hex {
			d, err := strconv.ParseInt(string(r), 16, 16)
			if err != nil {
				return 0, false
			}
			// Double the nibble into a full byte, then shift the value up.
			v = v<<8 | int(d)<<4 | int(d)
		}
		return v, true
	default:
		return 0, false
	}
}

// colorNames returns the named colours a viewer may type, for the reward prompt
// and dashboard help. Order is stable for a predictable UI.
func colorNames() []string {
	return []string{
		"red", "orange", "yellow", "green", "lime", "teal", "cyan",
		"blue", "navy", "purple", "violet", "indigo", "pink", "magenta",
		"white", "warm", "gold",
	}
}
