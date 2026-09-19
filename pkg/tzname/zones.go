// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package tzname

// resolveFullName is resolution step 3: an exact IANA zone name,
// case-insensitive, including link names ("asia/calcutta", "us/eastern").
func resolveFullName(normalized string) (Match, bool) {
	zone, ok := getIndex().byFullName[normalized]
	if !ok {
		return Match{}, false
	}
	return matchForZone(zone, segmentLabel(zone))
}

// resolveSegment is resolution step 4: the bare last path segment of a
// CANONICAL zone ("toronto", "new york", "ho chi minh").
func resolveSegment(normalized string) (Match, bool) {
	zone, ok := getIndex().bySegment[normalized]
	if !ok {
		return Match{}, false
	}
	return matchForZone(zone, segmentLabel(zone))
}
