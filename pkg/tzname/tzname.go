// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package tzname

import (
	"time"

	// sesame's distroless image has no zoneinfo; this embed is its only zone database.
	_ "time/tzdata"
)

//go:generate go run ./gen

type Match struct {
	Loc   *time.Location
	Zone  string
	Label string
}

var resolvers = []func(string) (Match, bool){
	resolveOffset,
	resolveCurated,
	resolveRegion,
	resolveFullName,
	resolveSegment,
}

func Resolve(query string) (Match, bool) {
	normalized := Normalize(query)
	if normalized == "" {
		return Match{}, false
	}
	for _, resolve := range resolvers {
		if m, ok := resolve(normalized); ok {
			return m, true
		}
	}
	return Match{}, false
}

func matchForZone(zone, label string) (Match, bool) {
	loc, err := Load(zone)
	if err != nil {
		return Match{}, false
	}
	return Match{Loc: loc, Zone: zone, Label: label}, true
}

func Load(name string) (*time.Location, error) {
	return time.LoadLocation(name)
}
