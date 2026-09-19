// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package tzname

import (
	"fmt"
	"regexp"
	"strconv"
	"time"
)

// offsetPattern matches a fixed-UTC-offset query (already normalized:
// trimmed, single-spaced, lowercased). The sign is REQUIRED - a bare "5"
// falls through to the curated table and index as a plain miss rather than
// being guessed as an offset, since a viewer who wants "hour 5" doesn't
// exist but a viewer who fat-fingered a city name does, and a clean "unknown
// place" reply costs one retype while a silently-wrong offset costs a wrong
// answer nobody notices. Hours are 1-2 digits; minutes are either ":MM" or
// bare MM.
var offsetPattern = regexp.MustCompile(`^(?:(?:utc|gmt)\s?)?([+-])(\d{1,2})(?::?(\d{2}))?$`)

// validOffsetMinutes are the only minute values a real UTC offset uses
// (quarter-hour zones like Asia/Kolkata's +5:30 or Australia/Eucla's +8:45);
// anything else ("+5:20") is almost certainly a mistyped hour, not a novel
// offset, so it's rejected rather than accepted.
var validOffsetMinutes = map[string]bool{"00": true, "15": true, "30": true, "45": true}

// resolveOffset is resolution step 1: a fixed UTC offset written as utc+2,
// gmt-4, +5:30, +0530, or -7.
func resolveOffset(normalized string) (Match, bool) {
	groups := offsetPattern.FindStringSubmatch(normalized)
	if groups == nil {
		return Match{}, false
	}
	hours, ok := parseOffsetHours(groups[2])
	if !ok {
		return Match{}, false
	}
	minutes, ok := parseOffsetMinutes(groups[3])
	if !ok {
		return Match{}, false
	}
	return offsetMatch(groups[1] == "-", hours, minutes), true
}

// parseOffsetHours enforces the 0-14 range (Pacific/Kiritimati's +14 is the
// furthest ahead any real zone goes); the regex only guarantees 1-2 digits.
func parseOffsetHours(s string) (int, bool) {
	h, err := strconv.Atoi(s)
	if err != nil {
		return 0, false
	}
	return h, withinOffsetHours(h)
}

// withinOffsetHours is the real-world bound: no zone sits further than 14
// hours from UTC (Pacific/Kiritimati), so anything past it is a typo.
func withinOffsetHours(h int) bool {
	return h >= 0 && h <= 14
}

// parseOffsetMinutes treats an absent minute group as :00, and rejects
// anything present but off the quarter-hour grid.
func parseOffsetMinutes(s string) (int, bool) {
	if s == "" {
		return 0, true
	}
	if !validOffsetMinutes[s] {
		return 0, false
	}
	m, _ := strconv.Atoi(s) // digit shape already checked by the regex and the set above
	return m, true
}

// offsetMatch builds a Match straight from time.FixedZone: an offset has no
// IANA identity to load, so unlike every other resolver step it can't fail
// past this point.
func offsetMatch(negative bool, hours, minutes int) Match {
	secs := hours*3600 + minutes*60
	sign := "+"
	if negative {
		sign, secs = "-", -secs
	}
	label := offsetLabel(sign, hours, minutes)
	return Match{Loc: time.FixedZone(label, secs), Zone: label, Label: label}
}

// offsetLabel omits the minutes term when it's zero ("UTC+2", not
// "UTC+2:00") and never zero-pads the hour ("UTC+5:30", not "UTC+05:30").
func offsetLabel(sign string, hours, minutes int) string {
	if minutes == 0 {
		return "UTC" + sign + strconv.Itoa(hours)
	}
	return fmt.Sprintf("UTC%s%d:%02d", sign, hours, minutes)
}
