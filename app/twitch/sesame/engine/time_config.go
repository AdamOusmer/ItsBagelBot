// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"strings"
	"time"

	"ItsBagelBot/pkg/tzname"
)

const TimeModuleName = "time"

type TimeModuleConfig struct {
	Timezone      string `json:"timezone"`
	Format        string `json:"format"`
	Message       string `json:"message"`
	LookupMessage string `json:"lookupMessage"`
}

func (c TimeModuleConfig) Zone() (*time.Location, bool) {
	tz := strings.TrimSpace(c.Timezone)
	if tz == "" {
		return nil, false
	}
	loc, err := tzname.Load(tz)
	return loc, err == nil
}

func FormatClock(t time.Time, format string) string {
	if format == "24" {
		return t.Format("15:04")
	}
	return t.Format("3:04 PM")
}
