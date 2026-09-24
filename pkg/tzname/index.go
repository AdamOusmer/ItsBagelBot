// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package tzname

import (
	"strings"
	"sync"
)

type zoneIndex struct {
	byFullName map[string]string
	bySegment  map[string]string
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

func foldZoneKey(s string) string {
	return strings.ReplaceAll(strings.ToLower(s), "_", " ")
}

func lastSegment(zone string) string {
	if i := strings.LastIndex(zone, "/"); i >= 0 {
		return zone[i+1:]
	}
	return zone
}

func segmentLabel(zone string) string {
	return strings.ReplaceAll(lastSegment(zone), "_", " ")
}
