// Package i18n is a tiny, dependency-free catalog for sesame's built-in (system)
// chat output. The broadcaster's locale rides the module Context
// (module.Context.Locale), sourced from the Valkey user projection, so system
// commands answer in the broadcaster's console language.
//
// It mirrors the console/site i18n set: add a locale here and to the users
// service's supportedLocales map together. English is the source of truth and
// every unknown locale or key falls back to it.
package i18n

// DefaultLocale is used when a message's locale is empty or unknown.
const DefaultLocale = "en"

// catalog[locale][key] -> template. Templates may carry fmt verbs (see T).
var catalog = map[string]map[string]string{
	"en": {
		"ping":         "Pong! ItsBagelBot has been up for %s",
		"bagels_ready": "/me I brought the bagels! 🥯",

		"cmd.added":            "@{user} the command {command} has been added",
		"cmd.removed":          "@{user} the command {command} has been removed",
		"cmd.modified":         "@{user} the command {command} has been modified",
		"cmd.err.usage":        "Usage: !cmd <add|edit|remove> <name> [response]",
		"cmd.err.exists":       "@{user} the command {command} already exists, use !cmd edit",
		"cmd.err.not_found":    "@{user} the command {command} was not found, use !cmd add",
		"cmd.err.missing_resp": "@{user} please provide a response for the command",
	},
	"fr": {
		"ping":         "Pong ! ItsBagelBot est actif depuis %s",
		"bagels_ready": "/me J'ai ramené les bagels ! 🥯",

		"cmd.added":            "@{user} la commande {command} a été ajoutée",
		"cmd.removed":          "@{user} la commande {command} a été supprimée",
		"cmd.modified":         "@{user} la commande {command} a été modifiée",
		"cmd.err.usage":        "Utilisation : !cmd <add|edit|remove> <nom> [réponse]",
		"cmd.err.exists":       "@{user} la commande {command} existe déjà, utilisez !cmd edit",
		"cmd.err.not_found":    "@{user} la commande {command} n'a pas été trouvée, utilisez !cmd add",
		"cmd.err.missing_resp": "@{user} veuillez fournir une réponse pour la commande",
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
