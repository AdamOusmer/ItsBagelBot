// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package i18n

import (
	"ItsBagelBot/locales"
	"ItsBagelBot/pkg/codec"
	"io/fs"
	"path"
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

const (
	manifestFile  = "manifest.json"
	chatDir       = "chat"
	rootNamespace = "index"
)

var supported = mustLoadManifest(locales.FS)

var catalog = mustLoadCatalogs(locales.FS)

func mustLoadManifest(fsys fs.FS) []string {
	b, err := fs.ReadFile(fsys, manifestFile)
	if err != nil {
		panic("i18n: cannot read " + manifestFile + ": " + err.Error())
	}
	var codes []string
	if err := codec.Unmarshal(b, &codes); err != nil {
		panic("i18n: malformed " + manifestFile + ": " + err.Error())
	}
	sort.Strings(codes)
	return codes
}

type namespaceFile struct {
	path   string
	prefix string
}

func newNamespaceFile(dir fs.DirEntry, file fs.DirEntry) namespaceFile {
	f := namespaceFile{path: path.Join(dir.Name(), chatDir, file.Name())}
	if namespace := strings.TrimSuffix(file.Name(), ".json"); namespace != rootNamespace {
		f.prefix = namespace + "."
	}
	return f
}

func (f namespaceFile) read(fsys fs.FS) map[string]string {
	b, err := fs.ReadFile(fsys, f.path)
	if err != nil {
		panic("i18n: cannot read " + f.path + ": " + err.Error())
	}
	var entries map[string]string
	if err := codec.Unmarshal(b, &entries); err != nil {
		panic("i18n: malformed " + f.path + ": " + err.Error())
	}
	return entries
}

func (f namespaceFile) mergeInto(table, entries map[string]string) {
	for key, tmpl := range entries {
		full := f.prefix + key
		if _, dup := table[full]; dup {
			panic("i18n: duplicate key " + full + " in " + f.path)
		}
		table[full] = strings.ReplaceAll(tmpl, dashboardToken, DashboardURL)
	}
}

func mustLoadCatalogs(fsys fs.FS) map[string]map[string]string {
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		panic("i18n: cannot read locales root: " + err.Error())
	}
	out := make(map[string]map[string]string, len(entries))
	for _, e := range entries {
		if e.IsDir() && hasChatCatalog(fsys, e) {
			out[e.Name()] = mustLoadCatalog(fsys, e)
		}
	}
	if _, ok := out[DefaultLocale]; !ok {
		panic("i18n: missing required catalog " + path.Join(DefaultLocale, chatDir))
	}
	return out
}

func hasChatCatalog(fsys fs.FS, locale fs.DirEntry) bool {
	info, err := fs.Stat(fsys, path.Join(locale.Name(), chatDir))
	return err == nil && info.IsDir()
}

func mustLoadCatalog(fsys fs.FS, locale fs.DirEntry) map[string]string {
	dir := path.Join(locale.Name(), chatDir)
	files, err := fs.ReadDir(fsys, dir)
	if err != nil {
		panic("i18n: cannot read " + dir + ": " + err.Error())
	}
	table := make(map[string]string)
	for _, f := range files {
		if f.IsDir() || path.Ext(f.Name()) != ".json" {
			continue
		}
		file := newNamespaceFile(locale, f)
		file.mergeInto(table, file.read(fsys))
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
	count := defaultSeriesCount(seriesPrefix(prefix))
	if count == 0 {
		panic("i18n: missing default series " + prefix)
	}
	series := make([]string, count)
	for index := range series {
		value, ok := catalog[DefaultLocale][prefix+"."+strconv.Itoa(index)]
		if !ok {
			panic("i18n: gap in default series " + prefix + " at " + strconv.Itoa(index))
		}
		series[index] = value
	}
	return series
}

type seriesPrefix string

func defaultSeriesCount(prefix seriesPrefix) int {
	count := 0
	for key := range catalog[DefaultLocale] {
		suffix, ok := strings.CutPrefix(key, string(prefix)+".")
		if !ok {
			continue
		}
		index, err := strconv.Atoi(suffix)
		if err == nil && index >= 0 {
			count++
		}
	}
	return count
}
