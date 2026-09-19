// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"strings"
	"time"

	// The embedded tzdata table now rides in via pkg/tzname (its blank import
	// of time/tzdata), so LoadLocation below resolves through tzname.Load
	// instead of pulling in a second copy of the same side-effect import.
	"ItsBagelBot/pkg/tzname"
)

// TimeModuleName is the per-broadcaster row that gates the Local Time module —
// the same row !time checks and the {time} response token is mounted on.
const TimeModuleName = "time"

// TimeModuleConfig is the Local Time module's dashboard configuration.
// Timezone is an IANA zone name ("America/Toronto") the dashboard suggests from
// the viewer's browser (Intl.DateTimeFormat, computed client-side only —
// nothing is stored until the broadcaster saves). Format selects the clock
// face: "24" for 15:04, anything else the 12-hour default. Message is the !time
// reply template, which the token does not use.
//
// It lives in the engine rather than in the modules package because the {time}
// token reads the same blob !time does, and the engine cannot import modules
// (modules imports the engine). Two structs over one JSON blob would be two
// spellings free to drift, and a drifted field name decodes to the empty string
// silently — the token would report an unset timezone on a channel that has one.
type TimeModuleConfig struct {
	Timezone string `json:"timezone"`
	Format   string `json:"format"`
	Message  string `json:"message"`
	// LookupMessage is the !time <place> reply template. Empty means the
	// module's built-in default; unlike Message, the {time} token never
	// reads this field, since the token only ever renders the home zone.
	LookupMessage string `json:"lookupMessage"`
}

// Zone loads the configured timezone. ok=false means the broadcaster has not
// set one yet, or set one the tz database does not know.
func (c TimeModuleConfig) Zone() (*time.Location, bool) {
	tz := strings.TrimSpace(c.Timezone)
	if tz == "" {
		return nil, false
	}
	loc, err := tzname.Load(tz)
	return loc, err == nil
}

// FormatClock renders one instant on the configured clock face: "24" gives
// 15:04, anything else the 12-hour default (3:04 PM). It is shared by !time
// and the {time} token so the two can never print the same moment differently.
func FormatClock(t time.Time, format string) string {
	if format == "24" {
		return t.Format("15:04")
	}
	return t.Format("3:04 PM")
}
