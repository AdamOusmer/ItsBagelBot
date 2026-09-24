// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package scope

import (
	"strings"
	"time"

	"ItsBagelBot/internal/domain/i18n"
)

const dateLayout = "2006-01-02"

func (p Pure) now() time.Time {
	if p.Now == nil {
		return time.Now()
	}
	return p.Now()
}

func (p Pure) countdown(payload string) string {
	at, ok := parseInstant(payload)
	if !ok {
		return ""
	}
	return p.humanize(at.Sub(p.now()))
}

func (p Pure) countup(payload string) string {
	at, ok := parseInstant(payload)
	if !ok {
		return ""
	}
	return p.humanize(p.now().Sub(at))
}

func (p Pure) humanize(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	return i18n.HumanizeDuration(p.Locale, d)
}

func parseInstant(payload string) (time.Time, bool) {
	text := strings.TrimSpace(payload)
	if at, err := time.Parse(time.RFC3339, text); err == nil {
		return at, true
	}
	at, err := time.ParseInLocation(dateLayout, text, time.UTC)
	return at, err == nil
}
