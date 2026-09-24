// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package tzname

import (
	"fmt"
	"regexp"
	"strconv"
	"time"
)

var offsetPattern = regexp.MustCompile(`^(?:(?:utc|gmt)\s?)?([+-])(\d{1,2})(?::?(\d{2}))?$`)

const maxUTCOffsetHours = 14

var validOffsetMinutes = map[string]bool{"00": true, "15": true, "30": true, "45": true}

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

func parseOffsetHours(s string) (int, bool) {
	h, err := strconv.Atoi(s)
	if err != nil {
		return 0, false
	}
	return h, withinOffsetHours(h)
}

func withinOffsetHours(h int) bool {
	return h >= 0 && h <= maxUTCOffsetHours
}

func parseOffsetMinutes(s string) (int, bool) {
	if s == "" {
		return 0, true
	}
	if !validOffsetMinutes[s] {
		return 0, false
	}
	m, _ := strconv.Atoi(s)
	return m, true
}

func offsetMatch(negative bool, hours, minutes int) Match {
	secs := hours*3600 + minutes*60
	sign := "+"
	if negative {
		sign, secs = "-", -secs
	}
	label := offsetLabel(sign, hours, minutes)
	return Match{Loc: time.FixedZone(label, secs), Zone: label, Label: label}
}

func offsetLabel(sign string, hours, minutes int) string {
	if minutes == 0 {
		return "UTC" + sign + strconv.Itoa(hours)
	}
	return fmt.Sprintf("UTC%s%d:%02d", sign, hours, minutes)
}
