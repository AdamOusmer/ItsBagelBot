// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package i18n

import (
	"ItsBagelBot/pkg/codec"
	"embed"
	"sort"
	"strconv"
	"strings"
)

const DefaultLocale = "en"

const DashboardURL = "https://dashboard.itsbagelbot.com"

const dashboardToken = "{dashboard_url}"

const (
	KeyReauthRevokedTitle = "reauth.revoked.title"
	KeyReauthRevokedBody  = "reauth.revoked.body"
	KeyReauthRevokedChat  = "reauth.revoked.chat"

	KeyGrantDeadTitle = "grant.dead.title"
	KeyGrantDeadBody  = "grant.dead.body"
	KeyGrantDeadChat  = "grant.dead.chat"

	KeyBotBannedTitle = "bot.banned.title"
	KeyBotBannedBody  = "bot.banned.body"
)

//go:embed locales.json locales/*.json
var i18nFS embed.FS

var supported = mustLoadManifest()

var catalog = mustLoadCatalogs()

func mustLoadManifest() []string {
	const name = "locales.json"
	b, err := i18nFS.ReadFile(name)
	if err != nil {
		panic("i18n: cannot read " + name + ": " + err.Error())
	}
	var codes []string
	if err := codec.Unmarshal(b, &codes); err != nil {
		panic("i18n: malformed " + name + ": " + err.Error())
	}
	sort.Strings(codes)
	return codes
}

func mustLoadCatalogs() map[string]map[string]string {
	entries, err := i18nFS.ReadDir("locales")
	if err != nil {
		panic("i18n: cannot read locales dir: " + err.Error())
	}
	out := make(map[string]map[string]string, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		locale := strings.TrimSuffix(e.Name(), ".json")
		out[locale] = mustLoadCatalog(locale)
	}
	if _, ok := out[DefaultLocale]; !ok {
		panic("i18n: missing required catalog locales/" + DefaultLocale + ".json")
	}
	return out
}

func mustLoadCatalog(locale string) map[string]string {
	name := "locales/" + locale + ".json"
	b, err := i18nFS.ReadFile(name)
	if err != nil {
		panic("i18n: cannot read " + name + ": " + err.Error())
	}
	var table map[string]string
	if err := codec.Unmarshal(b, &table); err != nil {
		panic("i18n: malformed " + name + ": " + err.Error())
	}
	for key, tmpl := range table {
		table[key] = strings.ReplaceAll(tmpl, dashboardToken, DashboardURL)
	}
	return table
}

func Supported(code string) bool {
	for _, c := range supported {
		if c == code {
			return true
		}
	}
	return false
}

func List() []string {
	out := make([]string, len(supported))
	copy(out, supported)
	return out
}

func Missing(locale string) []string {
	var missing []string
	for key := range catalog[DefaultLocale] {
		if _, ok := catalog[locale][key]; !ok {
			missing = append(missing, key)
		}
	}
	sort.Strings(missing)
	return missing
}

func Gaps() map[string][]string {
	gaps := make(map[string][]string, len(supported))
	for _, locale := range supported {
		if locale == DefaultLocale {
			continue
		}
		gaps[locale] = Missing(locale)
	}
	return gaps
}

func Locales() []string {
	out := make([]string, 0, len(catalog))
	for locale := range catalog {
		out = append(out, locale)
	}
	sort.Strings(out)
	return out
}

func T(locale, key string) string {
	if m, ok := catalog[locale]; ok {
		if s, ok := m[key]; ok {
			return s
		}
	}
	if s, ok := catalog[DefaultLocale][key]; ok {
		return s
	}
	return key
}

// DefaultSeries returns an ordered set of English templates named prefix.0,
// prefix.1, and so on. The English catalog defines both the copy and the size
// of the set; a gap is a broken source catalog and fails at startup.
func DefaultSeries(prefix string) []string {
	start := prefix + "."
	values := make(map[int]string)
	for key, value := range catalog[DefaultLocale] {
		if !strings.HasPrefix(key, start) {
			continue
		}
		index, err := strconv.Atoi(strings.TrimPrefix(key, start))
		if err == nil && index >= 0 {
			values[index] = value
		}
	}
	if len(values) == 0 {
		panic("i18n: missing default series " + prefix)
	}
	series := make([]string, len(values))
	for index := range series {
		value, ok := values[index]
		if !ok {
			panic("i18n: gap in default series " + prefix + " at " + strconv.Itoa(index))
		}
		series[index] = value
	}
	return series
}
