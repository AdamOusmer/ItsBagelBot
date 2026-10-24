// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package tzname

import (
	"sync"
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

var loaded sync.Map

// Load caches successes only, so the cache is bounded by the zone database.
func Load(name string) (*time.Location, error) {
	if loc, ok := loaded.Load(name); ok {
		return loc.(*time.Location), nil
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		return nil, err
	}
	loaded.Store(name, loc)
	return loc, nil
}
