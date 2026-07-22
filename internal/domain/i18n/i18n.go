// Package i18n is a tiny, dependency-free catalog for the Go services'
// user-facing chat and notification copy. sesame's built-in (system) commands
// take the broadcaster's locale from the module Context (module.Context.Locale),
// sourced from the Valkey user projection; outgress resolves it over the users
// state_get RPC. Either way system output answers in the broadcaster's console
// language.
//
// It lives under internal/domain so both services share one catalog rather than
// each carrying its own table. It stays dependency-free on purpose: nothing here
// may import a service package or pkg/bus, so locale RESOLUTION belongs to the
// caller and only the lookup lives here.
//
// The copy is pure data. locales.json lists the supported codes (the backend
// validation source of truth) and each locales/<code>.json holds that language's
// flat key->template map; both are embedded and parsed once at process start.
// Adding a language is therefore a data-only change: append the code to
// locales.json and drop locales/<code>.json (plus the matching console/web
// files). No Go edits, and no code runs from a translator's content. English is
// the source of truth: an unknown locale or a key missing from a locale falls
// back to it, and a partially translated locale is warned about at startup
// rather than failing the build. Malformed JSON, a missing English catalog, or
// an unreadable embedded file panics at init, so a broken drop is caught the
// moment the process starts rather than one lookup at a time.
package i18n

import (
	"embed"
	"encoding/json"
	"sort"
	"strings"
)

// DefaultLocale is used when a message's locale is empty or unknown.
const DefaultLocale = "en"

// DashboardURL is where a streamer re-consents. The JSON catalogs carry the
// placeholder token in its place and it is expanded to this value as the
// catalogs are parsed, so the environment-specific URL lives in one Go constant
// instead of being copied into every translation (where a translator could
// mangle it).
const DashboardURL = "https://dashboard.itsbagelbot.com"

// dashboardToken is the literal placeholder the JSON catalogs use for
// DashboardURL; expand replaces it at parse time.
const dashboardToken = "{dashboard_url}"

// Keys for the copy shared across services. Constants rather than raw strings
// because T falls back to returning the key itself, so a typo in one of these
// would post the literal key into a streamer's public chat.
const (
	KeyReauthRevokedTitle = "reauth.revoked.title"
	KeyReauthRevokedBody  = "reauth.revoked.body"
	KeyReauthRevokedChat  = "reauth.revoked.chat"

	KeyGrantDeadTitle = "grant.dead.title"
	KeyGrantDeadBody  = "grant.dead.body"
	KeyGrantDeadChat  = "grant.dead.chat"
)

// i18nFS embeds the catalog data: the manifest plus one file per locale. The
// glob must match at least one file, so locales/en.json existing is a compile
// guarantee here and a runtime check in mustLoadCatalogs.
//
//go:embed locales.json locales/*.json
var i18nFS embed.FS

// supported is the sorted list of codes from locales.json, the source of truth
// for backend validation (Supported and List). It is deliberately separate from
// the catalog files: a code may be declared supported before its catalog is
// complete, and Gaps reports the shortfall.
var supported = mustLoadManifest()

// catalog[locale][key] -> template, parsed from locales/<code>.json with the
// dashboard token expanded to DashboardURL. Templates may carry fmt verbs (see
// T) and {name}-style placeholders the caller substitutes.
var catalog = mustLoadCatalogs()

// mustLoadManifest reads and sorts locales.json. It panics on a read or parse
// failure so a malformed manifest crashes the process at init rather than
// silently narrowing the supported set.
func mustLoadManifest() []string {
	const name = "locales.json"
	b, err := i18nFS.ReadFile(name)
	if err != nil {
		panic("i18n: cannot read " + name + ": " + err.Error())
	}
	var codes []string
	if err := json.Unmarshal(b, &codes); err != nil {
		panic("i18n: malformed " + name + ": " + err.Error())
	}
	sort.Strings(codes)
	return codes
}

// mustLoadCatalogs parses every locales/*.json file into the catalog. Extra
// catalog files beyond the manifest are allowed and load fine (Gaps only walks
// the manifest); the one hard requirement is the English catalog, whose absence
// panics because every fallback path depends on it.
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

// mustLoadCatalog parses one locale file and expands the dashboard token in
// every value. It panics (naming the file) on a read or parse failure.
func mustLoadCatalog(locale string) map[string]string {
	name := "locales/" + locale + ".json"
	b, err := i18nFS.ReadFile(name)
	if err != nil {
		panic("i18n: cannot read " + name + ": " + err.Error())
	}
	var table map[string]string
	if err := json.Unmarshal(b, &table); err != nil {
		panic("i18n: malformed " + name + ": " + err.Error())
	}
	for key, tmpl := range table {
		table[key] = strings.ReplaceAll(tmpl, dashboardToken, DashboardURL)
	}
	return table
}

// Supported reports whether code is in the manifest, so the users service can
// reject a bogus locale before persisting it.
func Supported(code string) bool {
	for _, c := range supported {
		if c == code {
			return true
		}
	}
	return false
}

// List returns a sorted copy of the manifest's supported codes.
func List() []string {
	out := make([]string, len(supported))
	copy(out, supported)
	return out
}

// Missing reports the keys present in the default locale but absent from
// locale, so startup can warn on a half-translated locale (a manifest code with
// no catalog file yields every English key) instead of letting T silently fall
// back to English in a French streamer's chat.
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

// Gaps maps each supported locale except English to its Missing keys. It is
// keyed by List minus en; a fully translated locale maps to an empty slice, so
// callers warn only where len > 0.
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

// Locales lists every locale whose catalog file loaded, sorted.
func Locales() []string {
	out := make([]string, 0, len(catalog))
	for locale := range catalog {
		out = append(out, locale)
	}
	sort.Strings(out)
	return out
}

// T returns the template for key in locale, falling back to English, then to
// the key itself so a missing entry is visible rather than blank. The returned
// string may contain fmt verbs; the caller applies fmt.Sprintf with the args.
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
