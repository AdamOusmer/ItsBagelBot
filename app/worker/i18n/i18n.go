// Package workeri18n is a tiny, dependency-free catalog for the worker's
// built-in (system) chat output. The broadcaster's locale rides the module
// Context (module.Context.Locale), sourced from the Valkey user projection, so
// system commands answer in the broadcaster's console language.
//
// It mirrors the console/site i18n set: add a locale here and to the users
// service's supportedLocales map together. English is the source of truth and
// every unknown locale or key falls back to it.
package workeri18n

// DefaultLocale is used when a message's locale is empty or unknown.
const DefaultLocale = "en"

// catalog[locale][key] -> template. Templates may carry fmt verbs (see T).
var catalog = map[string]map[string]string{
	"en": {
		"ping": "Pong! ItsBagelBot has been up for %s",
	},
	"fr": {
		"ping": "Pong ! ItsBagelBot est actif depuis %s",
	},
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
