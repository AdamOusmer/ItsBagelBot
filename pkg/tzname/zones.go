// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package tzname

func resolveFullName(normalized string) (Match, bool) {
	zone, ok := getIndex().byFullName[normalized]
	if !ok {
		return Match{}, false
	}
	return matchForZone(zone, segmentLabel(zone))
}

func resolveSegment(normalized string) (Match, bool) {
	zone, ok := getIndex().bySegment[normalized]
	if !ok {
		return Match{}, false
	}
	return matchForZone(zone, segmentLabel(zone))
}
