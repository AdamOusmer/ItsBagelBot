// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package zonelist

import (
	"archive/zip"
	"fmt"
	"os/exec"
	"runtime"
	"sort"
	"strings"
)

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

var linkPrefixes = []string{"US/", "Canada/", "Brazil/", "Chile/", "Mexico/"}

var skipPrefixes = []string{"Etc/", "posix/", "right/", "SystemV"}

var skipExact = map[string]bool{"Factory": true, "posixrules": true, "localtime": true}

type Zones struct {
	Canonical []string
	Links     []string
}

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
