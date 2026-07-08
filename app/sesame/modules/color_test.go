package modules

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseColorNamed(t *testing.T) {
	cases := map[string]int{
		"red":     0xFF0000,
		"BLUE":    0x0066FF, // case-insensitive
		" green ": 0x00C000, // trimmed
		"magenta": 0xFF00FF,
		"white":   0xFFFFFF,
	}
	for in, want := range cases {
		rgb, ok := parseColor(in)
		assert.True(t, ok, "parseColor(%q) should match", in)
		assert.Equal(t, want, rgb, "parseColor(%q)", in)
	}
}

func TestParseColorHex(t *testing.T) {
	cases := map[string]int{
		"#00ccff": 0x00CCFF,
		"00ccff":  0x00CCFF,
		"#FFF":    0xFFFFFF, // short form doubles nibbles
		"f80":     0xFF8800,
		"#000000": 0x000000,
	}
	for in, want := range cases {
		rgb, ok := parseColor(in)
		assert.True(t, ok, "parseColor(%q) should parse", in)
		assert.Equal(t, want, rgb, "parseColor(%q)", in)
	}
}

func TestParseColorRejectsGarbage(t *testing.T) {
	for _, in := range []string{"", "   ", "notacolor", "#12", "12345", "#gggggg", "#1234567", "rgb(1,2,3)"} {
		_, ok := parseColor(in)
		assert.False(t, ok, "parseColor(%q) should be rejected", in)
	}
}

func TestColorNamesAllParse(t *testing.T) {
	// Every advertised name must actually resolve, or the reward prompt would
	// suggest colours the module then refunds.
	for _, name := range colorNames() {
		_, ok := parseColor(name)
		assert.True(t, ok, "advertised colour %q must parse", name)
	}
}
