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
		"cmd.link":             "@{user} browse {channel}'s commands here: {url}",
		"cmd.err.usage":        "Usage: !cmd <add|edit|remove> <name> [response]",
		"cmd.err.exists":       "@{user} the command {command} already exists, use !cmd edit",
		"cmd.err.not_found":    "@{user} the command {command} was not found, use !cmd add",
		"cmd.err.missing_resp": "@{user} please provide a response for the command",

		"queue.status.open":      "The queue is open with {count} waiting. Type !join to get in line.",
		"queue.status.closed":    "The queue is closed. {count} still waiting to play.",
		"queue.opened":           "The queue is now open! Type !join to get in line.",
		"queue.closed":           "The queue is now closed to new joins.",
		"queue.cleared":          "The queue has been cleared.",
		"queue.join.ok":          "@{user} you joined the queue at position #{pos}.",
		"queue.join.already":     "@{user} you are already in the queue at position #{pos}.",
		"queue.join.closed":      "@{user} the queue is closed right now.",
		"queue.leave.ok":         "@{user} you left the queue.",
		"queue.leave.not_in":     "@{user} you are not in the queue.",
		"queue.next":             "@{target} you are up next! ({count} still waiting)",
		"queue.next.empty":       "The queue is empty.",
		"queue.remove.ok":        "@{target} was removed from the queue.",
		"queue.remove.not_found": "@{target} was not in the queue.",
		"queue.remove.usage":     "Usage: !queue remove <user>",
		"queue.list":             "Next up: {list}",
		"queue.list.more":        "Next up: {list} (+{count} more)",
		"queue.list.empty":       "The queue is empty.",
		"queue.err.usage":        "Usage: !queue <open|close|next|list|remove|clear> — or !join / !leave / !list",

		"loyalty.points":              "@{user} you have {points} {name} and {hours} hours watched.",
		"loyalty.points.adjusted":     "@{target} now has {points} {name}.",
		"loyalty.points.unknown":      "@{user} I haven't seen {target} in this channel yet.",
		"loyalty.points.usage":        "Usage: !points — or !points <set|add> <user> <amount>",
		"loyalty.counter.usage":       "Usage: !counter <name> — or !counter create <name> [user|user+command], !counter <add|set|reset|delete> <name> [value], !counter list",
		"loyalty.counter.show":        "Counter {counter} is at {value}.",
		"loyalty.counter.show.viewer": "@{user} your {counter} count is {value}.",
		"loyalty.counter.created":     "Counter {counter} created ({scope}).",
		"loyalty.counter.set":         "Counter {counter} is now {value}.",
		"loyalty.counter.reset":       "Counter {counter} has been reset.",
		"loyalty.counter.deleted":     "Counter {counter} has been deleted.",
		"loyalty.counter.list":        "Counters: {list}",
		"loyalty.counter.list.empty":  "No counters yet. Create one with !counter create <name>.",
		"loyalty.counter.not_found":   "@{user} counter {counter} was not found.",
		"loyalty.counter.err":         "@{user} that didn't work, please try again.",
	},
	"fr": {
		"ping":         "Pong ! ItsBagelBot est actif depuis %s",
		"bagels_ready": "/me J'ai ramené les bagels ! 🥯",

		"cmd.added":            "@{user} la commande {command} a été ajoutée",
		"cmd.removed":          "@{user} la commande {command} a été supprimée",
		"cmd.modified":         "@{user} la commande {command} a été modifiée",
		"cmd.link":             "@{user} parcourez les commandes de {channel} ici : {url}",
		"cmd.err.usage":        "Utilisation : !cmd <add|edit|remove> <nom> [réponse]",
		"cmd.err.exists":       "@{user} la commande {command} existe déjà, utilisez !cmd edit",
		"cmd.err.not_found":    "@{user} la commande {command} n'a pas été trouvée, utilisez !cmd add",
		"cmd.err.missing_resp": "@{user} veuillez fournir une réponse pour la commande",

		"queue.status.open":      "La file est ouverte, {count} en attente. Tapez !join pour vous inscrire.",
		"queue.status.closed":    "La file est fermée. {count} encore en attente.",
		"queue.opened":           "La file est maintenant ouverte ! Tapez !join pour vous inscrire.",
		"queue.closed":           "La file est maintenant fermée aux nouvelles inscriptions.",
		"queue.cleared":          "La file a été vidée.",
		"queue.join.ok":          "@{user} vous avez rejoint la file en position n°{pos}.",
		"queue.join.already":     "@{user} vous êtes déjà dans la file en position n°{pos}.",
		"queue.join.closed":      "@{user} la file est fermée pour le moment.",
		"queue.leave.ok":         "@{user} vous avez quitté la file.",
		"queue.leave.not_in":     "@{user} vous n'êtes pas dans la file.",
		"queue.next":             "@{target} c'est à vous ! ({count} encore en attente)",
		"queue.next.empty":       "La file est vide.",
		"queue.remove.ok":        "@{target} a été retiré de la file.",
		"queue.remove.not_found": "@{target} n'était pas dans la file.",
		"queue.remove.usage":     "Utilisation : !queue remove <utilisateur>",
		"queue.list":             "À suivre : {list}",
		"queue.list.more":        "À suivre : {list} (+{count} autres)",
		"queue.list.empty":       "La file est vide.",
		"queue.err.usage":        "Utilisation : !queue <open|close|next|list|remove|clear> — ou !join / !leave / !list",

		"loyalty.points":              "@{user} vous avez {points} {name} et {hours} heures de visionnage.",
		"loyalty.points.adjusted":     "@{target} a maintenant {points} {name}.",
		"loyalty.points.unknown":      "@{user} je n'ai pas encore vu {target} sur cette chaîne.",
		"loyalty.points.usage":        "Utilisation : !points — ou !points <set|add> <utilisateur> <montant>",
		"loyalty.counter.usage":       "Utilisation : !counter <nom> — ou !counter create <nom> [user|user+command], !counter <add|set|reset|delete> <nom> [valeur], !counter list",
		"loyalty.counter.show":        "Le compteur {counter} est à {value}.",
		"loyalty.counter.show.viewer": "@{user} votre compteur {counter} est à {value}.",
		"loyalty.counter.created":     "Compteur {counter} créé ({scope}).",
		"loyalty.counter.set":         "Le compteur {counter} est maintenant à {value}.",
		"loyalty.counter.reset":       "Le compteur {counter} a été remis à zéro.",
		"loyalty.counter.deleted":     "Le compteur {counter} a été supprimé.",
		"loyalty.counter.list":        "Compteurs : {list}",
		"loyalty.counter.list.empty":  "Aucun compteur pour le moment. Créez-en un avec !counter create <nom>.",
		"loyalty.counter.not_found":   "@{user} le compteur {counter} n'a pas été trouvé.",
		"loyalty.counter.err":         "@{user} ça n'a pas fonctionné, veuillez réessayer.",
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
