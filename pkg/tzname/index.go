// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package tzname

import (
	"strings"
	"sync"
)

// zoneIndex maps a normalized query straight to the canonically-spelled zone
// name it should load, built once from the generated canonicalZones and
// linkZones slices (zones_gen.go) rather than walked linearly on every
// Resolve call.
type zoneIndex struct {
	// byFullName covers every zone (canonical and link): "asia/calcutta" ->
	// "Asia/Calcutta".
	byFullName map[string]string
	// bySegment covers canonical zones only: "new york" -> "America/New_York".
	// Links are excluded because a link's last segment routinely collides
	// with, or misleads about, its own canonical target - "eastern" from
	// US/Eastern would otherwise shadow "eastern" as a real place query
	// (there isn't one, but the point generalizes) instead of falling
	// through to a miss.
	bySegment map[string]string
}

var (
	indexOnce sync.Once
	index     zoneIndex
)

func getIndex() *zoneIndex {
	indexOnce.Do(buildIndex)
	return &index
}

func buildIndex() {
	index = zoneIndex{
		byFullName: make(map[string]string, len(canonicalZones)+len(linkZones)),
		bySegment:  make(map[string]string, len(canonicalZones)),
	}
	addFullNames(canonicalZones)
	addFullNames(linkZones)
	addSegments(canonicalZones)
}

func addFullNames(zones []string) {
	for _, zone := range zones {
		index.byFullName[foldZoneKey(zone)] = zone
	}
}

func addSegments(zones []string) {
	for _, zone := range zones {
		index.bySegment[foldZoneKey(lastSegment(zone))] = zone
	}
}

// foldZoneKey puts a stored zone name (or one of its segments) through the
// same underscore->space, lower-case folding Normalize applies to a query,
// so the two sides of every map lookup are directly comparable.
func foldZoneKey(s string) string {
	return strings.ReplaceAll(strings.ToLower(s), "_", " ")
}

func lastSegment(zone string) string {
	if i := strings.LastIndex(zone, "/"); i >= 0 {
		return zone[i+1:]
	}
	return zone
}

// segmentLabel is the display form of a zone's last path segment: underscore
// folded back to a space, original casing kept ("Ho_Chi_Minh" -> "Ho Chi
// Minh").
func segmentLabel(zone string) string {
	return strings.ReplaceAll(lastSegment(zone), "_", " ")
}
