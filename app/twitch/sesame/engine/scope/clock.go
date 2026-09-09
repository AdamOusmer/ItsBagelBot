// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package scope

import (
	"strings"
	"time"

	"ItsBagelBot/internal/domain/i18n"
)

// dateLayout is the second spelling {countdown}/{countup} accept, beside
// RFC3339.
//
// Decision record. RFC3339 alone was rejected: "2026-12-25" is what a
// broadcaster types for a stream anniversary or a game launch, and demanding
// "2026-12-25T00:00:00Z" for it turns a one-line token into a support
// question. A bare date carries no zone, so it is read as UTC midnight rather
// than in the channel's local zone: the channel zone lives in the Local Time
// module, which a broadcaster with {countdown} in a template may not have on,
// and a token whose answer silently changes when an unrelated module is
// toggled is worse than one that is off by a few hours near midnight.
const dateLayout = "2006-01-02"

// now is the clock the countdown tokens measure against, injectable so a test
// can pin "now" instead of asserting against a moving target.
func (p Pure) now() time.Time {
	if p.Now == nil {
		return time.Now()
	}
	return p.Now()
}

// countdown renders the time remaining until the payload's instant.
func (p Pure) countdown(payload string) string {
	at, ok := parseInstant(payload)
	if !ok {
		return ""
	}
	return p.humanize(at.Sub(p.now()))
}

// countup renders the time elapsed since the payload's instant.
func (p Pure) countup(payload string) string {
	at, ok := parseInstant(payload)
	if !ok {
		return ""
	}
	return p.humanize(p.now().Sub(at))
}

// humanize renders one span through the catalog's shared humanizer — the same
// one !uptime, !followage and !accountage print — so "3 days, 4 hours" reads
// identically wherever the bot says it.
//
// A span that has already passed clamps to zero rather than counting the
// other way: {countdown} counting UP after its date would silently become
// {countup}, and a template that read "3 days until launch" would then read
// "3 days until launch" again a week later with a number that means the
// opposite. The clamped value renders through the same humanizer as any
// other, so it reads "less than a minute" rather than a second spelling of
// zero invented here.
func (p Pure) humanize(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	return i18n.HumanizeDuration(p.Locale, d)
}

// parseInstant reads the payload as an RFC3339 timestamp, or as a bare
// YYYY-MM-DD date at UTC midnight. Anything else is not a date, and the token
// resolves to "" (so its fallback renders) rather than to a guess.
func parseInstant(payload string) (time.Time, bool) {
	text := strings.TrimSpace(payload)
	if at, err := time.Parse(time.RFC3339, text); err == nil {
		return at, true
	}
	at, err := time.ParseInLocation(dateLayout, text, time.UTC)
	return at, err == nil
}
