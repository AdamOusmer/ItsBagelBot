// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Package tzname resolves a viewer-typed place string ("!time tokyo", "!time
// +5:30", "!time montreal") to a *time.Location, for sesame's !time chat
// command. Pure and deterministic: no network calls, no I/O beyond the
// compiled-in zone table.
package tzname

import (
	"time"

	// sesame's runtime image is distroless/static - there is no
	// /usr/share/zoneinfo on it for time.LoadLocation to fall back to, so the
	// zone database has to ride inside the binary instead. This package is
	// the one place in the repo that pays for that (a few hundred KB), and
	// every other caller that needs a *time.Location goes through
	// tzname.Resolve rather than importing time/tzdata a second time.
	_ "time/tzdata"
)

//go:generate go run ./gen

// Match is one resolved place.
type Match struct {
	Loc *time.Location
	// Zone is the IANA name as matched: the canonical or link spelling a
	// full-name/segment hit resolved to ("America/New_York", or
	// "Asia/Calcutta" when the viewer typed the link form directly), the
	// curated table's target zone for an abbreviation/city hit, or
	// "UTC+5:30" for a raw offset (which has no IANA identity of its own).
	Zone string
	// Label is what sesame echoes to chat: a city name ("Toronto"), a named
	// offset ("Eastern Time"), or the offset text itself ("UTC+2").
	Label string
}

// resolvers runs in first-hit-wins order. Each step is its own function
// (CodeScene: cyclomatic <=9, nesting <=2 per function) rather than one
// branch-heavy dispatcher, and the order itself is the spec: a fixed offset
// is unambiguous and checked first, then the curated table (hand-picked for
// Twitch-chat prevalence beats the "first IANA name to alphabetically
// match"), then state/province/country names, then any exact IANA name,
// then a bare city name.
var resolvers = []func(string) (Match, bool){
	resolveOffset,
	resolveCurated,
	resolveRegion,
	resolveFullName,
	resolveSegment,
}

// Resolve maps a viewer-typed place to a Match. ok=false means unknown.
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

// matchForZone loads zone and pairs it with label, shared by every resolver
// step that ends at a real IANA identifier (curated, full-name, segment -
// resolveOffset builds its Match directly since time.FixedZone can't fail).
func matchForZone(zone, label string) (Match, bool) {
	loc, err := Load(zone)
	if err != nil {
		return Match{}, false
	}
	return Match{Loc: loc, Zone: zone, Label: label}, true
}

// Load resolves an IANA zone name to a *time.Location. It exists so the
// engine's home-zone load goes through the package that owns the embedded
// tzdata import (the blank import above): every caller that needs a zone by
// name calls tzname.Load instead of importing time/tzdata a second time,
// which would turn a real dependency into a side-effect import two packages
// both have to remember to keep.
func Load(name string) (*time.Location, error) {
	return time.LoadLocation(name)
}
