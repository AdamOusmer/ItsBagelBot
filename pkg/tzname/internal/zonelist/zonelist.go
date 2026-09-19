// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Package zonelist lists the IANA zone names Go's stdlib ships in
// $GOROOT/lib/time/zoneinfo.zip, split into canonical zones and link
// (alias/legacy) names. It exists as its own package, rather than living
// inside gen/main.go, so both the generator and the golden test
// (tzname_golden_test.go) can call the exact same classification logic:
// a generator-only copy would drift from what the test checks against.
package zonelist

import (
	"archive/zip"
	"fmt"
	"os/exec"
	"runtime"
	"sort"
	"strings"
)

// legacyLinks names zones that are technically their own zip entries but that
// tzdata treats as backward-compatibility aliases for a renamed or merged
// zone (see IANA's "backward" file). They don't share a naming pattern with
// the US/, Canada/, Brazil/, Chile/, Mexico/ prefixes or the single-segment
// legacy names, so there is no rule to derive them from - they're listed by
// hand, per the spec this package implements.
var legacyLinks = map[string]bool{
	"Asia/Calcutta": true, "Asia/Katmandu": true, "Asia/Rangoon": true,
	"Asia/Saigon": true, "Europe/Kiev": true, "Asia/Ulan_Bator": true,
	"Pacific/Enderbury": true, "Atlantic/Faeroe": true, "America/Godthab": true,
	"Asia/Dacca": true, "Asia/Macao": true, "Asia/Thimbu": true,
	"Asia/Ujung_Pandang": true, "Europe/Uzhgorod": true, "Europe/Zaporozhye": true,
	"Asia/Ashkhabad": true, "Asia/Chongqing": true, "Asia/Chungking": true,
	"Asia/Harbin": true, "Asia/Kashgar": true, "Asia/Tel_Aviv": true,
	"America/Buenos_Aires": true, "America/Catamarca": true, "America/Cordoba": true,
	"America/Jujuy": true, "America/Mendoza": true, "America/Rosario": true,
	"America/Indianapolis": true, "America/Fort_Wayne": true, "America/Knox_IN": true,
	"America/Louisville": true, "America/Montreal": true, "America/Shiprock": true,
	"America/Virgin": true, "America/Atka": true, "America/Ensenada": true,
	"America/Porto_Acre": true, "America/Santa_Isabel": true, "America/Coral_Harbour": true,
	"Antarctica/South_Pole": true, "Australia/ACT": true, "Australia/Canberra": true,
	"Australia/NSW": true, "Australia/North": true, "Australia/Queensland": true,
	"Australia/South": true, "Australia/Tasmania": true, "Australia/Victoria": true,
	"Australia/West": true, "Australia/Yancowinna": true, "Australia/LHI": true,
	"Europe/Belfast": true, "Europe/Nicosia": true, "Europe/Tiraspol": true,
	"Pacific/Johnston": true, "Pacific/Ponape": true, "Pacific/Samoa": true,
	"Pacific/Truk": true, "Pacific/Yap": true, "Africa/Asmera": true,
	"Africa/Timbuktu": true, "Atlantic/Jan_Mayen": true,
}

// linkPrefixes are directory prefixes that are entirely made of country-local
// aliases for zones that already exist (and are named) under their normal
// Continent/City entry - e.g. US/Eastern duplicates America/New_York. Every
// entry in one of these trees is a link, with no exceptions worth carving out.
var linkPrefixes = []string{"US/", "Canada/", "Brazil/", "Chile/", "Mexico/"}

// skipPrefixes are zip entries that aren't zone names at all: Etc/ (its
// GMT+N/GMT-N sign convention is inverted from what a viewer means by "+N",
// so tzname's curated table and index cover it explicitly instead), and the
// POSIX/right-variant trees, which duplicate the normal tree under a
// different rule set (right/ adds leap seconds, posix/ forces POSIX sign
// conventions) and would otherwise double every zone name in the index.
var skipPrefixes = []string{"Etc/", "posix/", "right/", "SystemV"}

// skipExact are non-zone bookkeeping entries in the zip.
var skipExact = map[string]bool{"Factory": true, "posixrules": true, "localtime": true}

// Zones is the classified, sorted zone listing.
type Zones struct {
	Canonical []string
	Links     []string
}

// Load reads GOROOT's zoneinfo.zip and classifies every entry.
func Load() (Zones, error) {
	root, err := goroot()
	if err != nil {
		return Zones{}, err
	}
	path := root + "/lib/time/zoneinfo.zip"
	r, err := zip.OpenReader(path)
	if err != nil {
		return Zones{}, fmt.Errorf("open %s: %w", path, err)
	}
	defer func() { _ = r.Close() }()

	var z Zones
	for _, f := range r.File {
		name := f.Name
		if skip(name) {
			continue
		}
		if isLink(name) {
			z.Links = append(z.Links, name)
		} else {
			z.Canonical = append(z.Canonical, name)
		}
	}
	sort.Strings(z.Canonical)
	sort.Strings(z.Links)
	return z, nil
}

// goroot prefers runtime.GOROOT() - no subprocess, correct for the running
// binary - and falls back to "go env GOROOT" for a toolchain whose runtime
// build baked in a different (or empty) root than the go binary on PATH.
func goroot() (string, error) {
	if root := runtime.GOROOT(); root != "" {
		return root, nil
	}
	out, err := exec.Command("go", "env", "GOROOT").Output()
	if err != nil {
		return "", fmt.Errorf("go env GOROOT: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

func skip(name string) bool {
	if skipExact[name] {
		return true
	}
	for _, p := range skipPrefixes {
		if strings.HasPrefix(name, p) {
			return true
		}
	}
	return false
}

// isLink reports whether name is an alias/legacy zone rather than a
// canonical one: a bare single-segment name (Cuba, Japan, GMT+0 - none of
// these describe a place, they're all shorthands for a Continent/City zone),
// a country-local alias tree (US/Eastern, Canada/Yukon, ...), or one of the
// individually-renamed legacy names IANA keeps for backward compatibility.
func isLink(name string) bool {
	if !strings.Contains(name, "/") {
		return true
	}
	for _, p := range linkPrefixes {
		if strings.HasPrefix(name, p) {
			return true
		}
	}
	return legacyLinks[name]
}
