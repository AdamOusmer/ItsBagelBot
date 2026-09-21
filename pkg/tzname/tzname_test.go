// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package tzname

import (
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ItsBagelBot/pkg/tzname/internal/zonelist"
)

// wantMatch is the subset of a Match a test cares about; Loc is checked only
// for non-nilness (two *time.Location for the same zone name aren't required
// to be the same pointer, so comparing it by value isn't useful here).
type wantMatch struct {
	zone  string
	label string
}

func TestResolve(t *testing.T) {
	cases := []struct {
		name  string
		query string
		want  wantMatch // zero value + ok=false means "unknown"
		ok    bool
	}{
		// --- Step 1: offsets, accepted. ---
		{name: "offset utc+2", query: "utc+2", want: wantMatch{"UTC+2", "UTC+2"}, ok: true},
		{name: "offset utc space plus 2", query: "utc +2", want: wantMatch{"UTC+2", "UTC+2"}, ok: true},
		{name: "offset gmt-4", query: "gmt-4", want: wantMatch{"UTC-4", "UTC-4"}, ok: true},
		{name: "offset plus 5:30", query: "+5:30", want: wantMatch{"UTC+5:30", "UTC+5:30"}, ok: true},
		{name: "offset plus 0530", query: "+0530", want: wantMatch{"UTC+5:30", "UTC+5:30"}, ok: true},
		{name: "offset minus 7", query: "-7", want: wantMatch{"UTC-7", "UTC-7"}, ok: true},
		{name: "offset utc+0", query: "utc+0", want: wantMatch{"UTC+0", "UTC+0"}, ok: true},

		// --- Step 1: offsets, rejected (fall through to an overall miss). ---
		{name: "offset reject bare hour", query: "5", ok: false},
		{name: "offset reject out of range", query: "+15", ok: false},
		{name: "offset reject off quarter-hour", query: "+5:20", ok: false},
		{name: "offset reject truncated minute", query: "+5:3", ok: false},

		// --- Step 2: curated abbreviations. ---
		{name: "abbr est", query: "est", want: wantMatch{"America/New_York", "Eastern Time"}, ok: true},
		{name: "abbr cst", query: "cst", want: wantMatch{"America/Chicago", "Central Time"}, ok: true},
		{name: "abbr mst", query: "mst", want: wantMatch{"America/Denver", "Mountain Time"}, ok: true},
		{name: "abbr pst", query: "pst", want: wantMatch{"America/Los_Angeles", "Pacific Time"}, ok: true},
		{name: "abbr jst", query: "jst", want: wantMatch{"Asia/Tokyo", "Japan Time"}, ok: true},
		{name: "abbr ist", query: "ist", want: wantMatch{"Asia/Kolkata", "India Time"}, ok: true},
		{name: "abbr bst", query: "bst", want: wantMatch{"Europe/London", "British Time"}, ok: true},
		{name: "abbr hkt", query: "hkt", want: wantMatch{"Asia/Hong_Kong", "Hong Kong Time"}, ok: true},

		// --- Step 2: curated cities. ---
		{name: "city montreal", query: "montreal", want: wantMatch{"America/Toronto", "Montreal"}, ok: true},
		{name: "city sf", query: "sf", want: wantMatch{"America/Los_Angeles", "San Francisco"}, ok: true},
		{name: "city nyc", query: "nyc", want: wantMatch{"America/New_York", "New York"}, ok: true},
		{name: "city kiev resolves to kyiv", query: "kiev", want: wantMatch{"Europe/Kyiv", "Kyiv"}, ok: true},

		// --- Step 3: regions (states, provinces, countries). ---
		{name: "region texas", query: "texas", want: wantMatch{"America/Chicago", "Texas"}, ok: true},
		{name: "region florida majority eastern", query: "Florida", want: wantMatch{"America/New_York", "Florida"}, ok: true},
		{name: "region indiana", query: "indiana", want: wantMatch{"America/Indiana/Indianapolis", "Indiana"}, ok: true},
		{name: "region washington state", query: "washington state", want: wantMatch{"America/Los_Angeles", "Washington State"}, ok: true},
		{name: "region washington stays dc", query: "washington", want: wantMatch{"America/New_York", "Washington"}, ok: true},
		{name: "region ontario", query: "Ontario", want: wantMatch{"America/Toronto", "Ontario"}, ok: true},
		{name: "region bc", query: "bc", want: wantMatch{"America/Vancouver", "British Columbia"}, ok: true},
		{name: "region england", query: "england", want: wantMatch{"Europe/London", "England"}, ok: true},
		{name: "region japan", query: "japan", want: wantMatch{"Asia/Tokyo", "Japan"}, ok: true},
		{name: "region no-majority country usa", query: "usa", ok: false},
		{name: "region no-majority country canada", query: "canada", ok: false},
		{name: "region no-majority country australia", query: "australia", ok: false},

		// --- Step 4: exact IANA name, any case. ---
		{name: "iana uppercase", query: "EUROPE/PARIS", want: wantMatch{"Europe/Paris", "Paris"}, ok: true},

		// --- Step 4: link names. ---
		{name: "link asia/calcutta", query: "asia/calcutta", want: wantMatch{"Asia/Calcutta", "Calcutta"}, ok: true},
		{name: "link us/eastern", query: "us/eastern", want: wantMatch{"US/Eastern", "Eastern"}, ok: true},

		// --- Step 5: bare segment of a canonical zone. ---
		{name: "segment toronto", query: "toronto", want: wantMatch{"America/Toronto", "Toronto"}, ok: true},
		{name: "segment new york space", query: "new york", want: wantMatch{"America/New_York", "New York"}, ok: true},
		{name: "segment new york underscore", query: "new_york", want: wantMatch{"America/New_York", "New York"}, ok: true},
		{name: "segment buenos aires", query: "Buenos Aires", want: wantMatch{"America/Argentina/Buenos_Aires", "Buenos Aires"}, ok: true},
		{name: "segment ho chi minh", query: "ho chi minh", want: wantMatch{"Asia/Ho_Chi_Minh", "Ho Chi Minh"}, ok: true},

		// --- Accent folding (step 5, and step 2 for montreal above). ---
		{name: "accent sao paulo", query: "São Paulo", want: wantMatch{"America/Sao_Paulo", "Sao Paulo"}, ok: true},
		{name: "accent montreal", query: "Montréal", want: wantMatch{"America/Toronto", "Montreal"}, ok: true},
		{name: "accent zurich", query: "Zürich", want: wantMatch{"Europe/Zurich", "Zurich"}, ok: true},

		// --- Normalize hygiene: trailing punctuation, leading @. ---
		{name: "trailing punctuation", query: "tokyo?", want: wantMatch{"Asia/Tokyo", "Tokyo"}, ok: true},
		{name: "leading at", query: "@tokyo", want: wantMatch{"Asia/Tokyo", "Tokyo"}, ok: true},

		// --- Etc/* excluded from steps 4 and 5 (inverted-sign trap). ---
		{name: "etc gmt rejected", query: "Etc/GMT+3", ok: false},

		// --- Unknown. ---
		{name: "unknown narnia", query: "narnia", ok: false},
		{name: "unknown bare number", query: "5", ok: false},
		{name: "unknown empty", query: "", ok: false},
		{name: "unknown whitespace", query: "   ", ok: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := Resolve(tc.query)
			if !tc.ok {
				assert.False(t, ok, "Resolve(%q) = %+v, want a miss", tc.query, got)
				return
			}
			require.True(t, ok, "Resolve(%q): want a hit, got a miss", tc.query)
			assert.Equal(t, tc.want.zone, got.Zone, "Resolve(%q).Zone", tc.query)
			assert.Equal(t, tc.want.label, got.Label, "Resolve(%q).Label", tc.query)
			assert.NotNil(t, got.Loc, "Resolve(%q).Loc", tc.query)
		})
	}
}

// TestNormalizeCapsAt64Runes pins the truncation length itself, independent
// of what a 64-rune-or-longer query happens to resolve to.
func TestNormalizeCapsAt64Runes(t *testing.T) {
	long := strings.Repeat("é", 100) // multi-byte rune: also checks the cap counts runes, not bytes
	got := Normalize(long)
	assert.Len(t, []rune(got), 64)
	assert.Equal(t, strings.Repeat("e", 64), got)
}

// TestLoad pins Load as a thin, working wrapper over time.LoadLocation: a
// real zone loads, a bogus one errors.
func TestLoad(t *testing.T) {
	_, err := Load("America/Toronto")
	assert.NoError(t, err)
	_, err = Load("Nope/Nope")
	assert.Error(t, err)
}

// TestCuratedZonesLoad guards against a typo'd IANA name in curated.go: every
// entry has to actually load, or Resolve would silently miss for viewers who
// typed a perfectly good abbreviation or city name.
func TestCuratedZonesLoad(t *testing.T) {
	for query, entry := range regionZones {
		_, err := time.LoadLocation(entry.zone)
		assert.NoErrorf(t, err, "region entry %q -> %q does not load", query, entry.zone)
	}
	for query, entry := range curatedZones {
		_, err := time.LoadLocation(entry.zone)
		assert.NoErrorf(t, err, "curated entry %q -> %q does not load", query, entry.zone)
	}
}

// TestZonesGenCurrent is the golden test for zones_gen.go: it re-derives the
// zone lists from the running toolchain's own zoneinfo.zip and asserts they
// match what's checked in. A failure means either the Go toolchain's tzdata
// moved on (new zones added/renamed upstream) or zonelist's classification
// rules changed without regenerating - either way the fix is the same.
func TestZonesGenCurrent(t *testing.T) {
	current, err := zonelist.Load()
	require.NoError(t, err)

	const staleMsg = "pkg/tzname/zones_gen.go is stale relative to this toolchain's zoneinfo.zip; run go generate ./pkg/tzname"
	assert.Equal(t, current.Canonical, canonicalZones, staleMsg)
	assert.Equal(t, current.Links, linkZones, staleMsg)
}

// TestSegmentCollisions asserts that no two canonical zones share a bare
// last-path-segment key unless the curated table already intercepts that
// exact query at step 2 (see curated.go's doc comment for the one collision
// this repo's zoneinfo.zip has today: Asia/Istanbul vs Europe/Istanbul). A
// new collision means a step-4 lookup would start returning whichever zone
// happens to sort last, which is silent and wrong; the fix is always the
// same one curated.go documents: add the ambiguous name to curatedZones.
func TestSegmentCollisions(t *testing.T) {
	bySegment := map[string][]string{}
	for _, zone := range canonicalZones {
		key := foldZoneKey(lastSegment(zone))
		bySegment[key] = append(bySegment[key], zone)
	}

	var uncovered []string
	for key, zones := range bySegment {
		if len(zones) < 2 {
			continue
		}
		if _, curated := curatedZones[key]; curated {
			continue // step 2 shadows step 4 for this exact query, so the ambiguity never reaches resolveSegment
		}
		uncovered = append(uncovered, fmt.Sprintf("%s: %v", key, zones))
	}
	sort.Strings(uncovered)
	assert.Empty(t, uncovered, "last-segment collisions with no curated override (add the colliding name to curatedZones): %v", uncovered)
}

// TestRegionZonesDisjoint keeps regions.go minimal and its ordering honest:
// a region name must not duplicate a curated entry (curated.go's collision
// picks win by running first, so a duplicate would be dead) and must not
// shadow a canonical zone's last segment (which already resolves at step 5,
// so a duplicate would be redundant and a mismatch would be a silent
// override of IANA data).
func TestRegionZonesDisjoint(t *testing.T) {
	for name := range regionZones {
		_, curated := curatedZones[name]
		assert.Falsef(t, curated, "region %q duplicates a curated entry; drop it from regions.go", name)
		_, segment := index.bySegment[foldZoneKey(name)]
		assert.Falsef(t, segment, "region %q is already a canonical zone segment; drop it from regions.go", name)
	}
}
