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
// It mirrors the console/site i18n set: add a locale here and to the users
// service's supportedLocales map together. English is the source of truth and
// every unknown locale or key falls back to it.
package i18n

import "sort"

// DefaultLocale is used when a message's locale is empty or unknown.
const DefaultLocale = "en"

// DashboardURL is where a streamer re-consents; baked into the reauth and
// grant-dead copy below.
const DashboardURL = "https://dashboard.itsbagelbot.com"

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

		"quote.show":         "Quote #{num}: {text} ({date})",
		"quote.added":        "@{user} quote #{num} added.",
		"quote.edited":       "Quote #{num} updated.",
		"quote.removed":      "Quote #{num} removed.",
		"quote.not_found":    "Quote #{num} doesn't exist.",
		"quote.none":         "No quotes saved yet. Add one with !addquote <text>.",
		"quote.search.none":  "No quote matching \"{term}\".",
		"quote.edit.usage":   "Usage: !quote edit <number> <new text>",
		"quote.remove.usage": "Usage: !quote remove <number>",
		"quote.err.usage":    "Usage: !quote — !quote <number> — !quote <word> — !addquote <text> — !quote edit <number> <text> — !quote remove <number>",

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

		"lookup.not_user":         "@%s is not a Twitch user.",
		"followage.unavailable":   "Followage is unavailable right now.",
		"followage.broadcaster":   "@%s is the broadcaster.",
		"followage.not_following": "@%s is not following this channel.",
		"followage.followed":      "@%s has followed for %s.",
		"accountage.unavailable":  "Account age is unavailable right now.",
		"accountage.age":          "@%s's account is %s old.",

		"time.less_than_minute": "less than a minute",
		"time.year":             "year",
		"time.years":            "years",
		"time.month":            "month",
		"time.months":           "months",
		"time.day":              "day",
		"time.days":             "days",
		"time.hour":             "hour",
		"time.hours":            "hours",
		"time.minute":           "minute",
		"time.minutes":          "minutes",

		"external.retry": "still looking that up, try again in a moment",

		"mcsr.session.started": "session tracking just started (%s elo), ask again after a match!",
		"mcsr.unrated":         "unrated",

		"fortnite.shop.empty": "empty today",
		"fortnite.shop.more":  "+%d more",

		// Twitch revoked the grant outright: the streamer changed a password or
		// disconnected the app, and Twitch told us so over EventSub.
		"reauth.revoked.title": "Bot lost access to your channel",
		"reauth.revoked.body": "Twitch revoked the bot's authorization for your channel (this happens after a password change or if the app was disconnected). " +
			"Chat replies and alerts are paused. Log in at " + DashboardURL + " and reconnect your Twitch account to bring the bot back.",
		"reauth.revoked.chat": "The bot lost its Twitch authorization for this channel (password change or app disconnect). " +
			"Log in at " + DashboardURL + " to reconnect it.",

		// The stored grant simply stopped working: Twitch rejects the refresh
		// token but never revoked anything, so the app is still connected on
		// Twitch's side. Deliberately worded WITHOUT blaming the streamer, who
		// would otherwise go hunting in Twitch Connections and find nothing wrong.
		"grant.dead.title": "Reconnect your Twitch account",
		// The bell is read from inside the dashboard, so it points at the
		// settings page rather than repeating the URL.
		"grant.dead.body": "Some connections for your channel have expired. " +
			"Just head to settings and reconnect your account to fix the problem.",
		"grant.dead.chat": "Some connections for your channel have expired. " +
			"Just log in to the dashboard to fix the problem: " + DashboardURL,
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

		"quote.show":         "Citation n°{num} : {text} ({date})",
		"quote.added":        "@{user} citation n°{num} ajoutée.",
		"quote.edited":       "Citation n°{num} modifiée.",
		"quote.removed":      "Citation n°{num} supprimée.",
		"quote.not_found":    "La citation n°{num} n'existe pas.",
		"quote.none":         "Aucune citation pour le moment. Ajoutez-en une avec !addquote <texte>.",
		"quote.search.none":  "Aucune citation contenant « {term} ».",
		"quote.edit.usage":   "Utilisation : !quote edit <numéro> <nouveau texte>",
		"quote.remove.usage": "Utilisation : !quote remove <numéro>",
		"quote.err.usage":    "Utilisation : !quote — !quote <numéro> — !quote <mot> — !addquote <texte> — !quote edit <numéro> <texte> — !quote remove <numéro>",

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

		"lookup.not_user":         "@%s n'est pas un utilisateur Twitch.",
		"followage.unavailable":   "Le suivi n'est pas disponible pour le moment.",
		"followage.broadcaster":   "@%s est le diffuseur.",
		"followage.not_following": "@%s ne suit pas cette chaîne.",
		"followage.followed":      "@%s suit la chaîne depuis %s.",
		"accountage.unavailable":  "L'âge du compte n'est pas disponible pour le moment.",
		"accountage.age":          "Le compte de @%s existe depuis %s.",

		"time.less_than_minute": "moins d'une minute",
		"time.year":             "an",
		"time.years":            "ans",
		"time.month":            "mois",
		"time.months":           "mois",
		"time.day":              "jour",
		"time.days":             "jours",
		"time.hour":             "heure",
		"time.hours":            "heures",
		"time.minute":           "minute",
		"time.minutes":          "minutes",

		"external.retry": "recherche en cours, réessayez dans un instant",

		"mcsr.session.started": "le suivi de session vient de démarrer (%s elo), redemandez après une partie !",
		"mcsr.unrated":         "non classé",

		"fortnite.shop.empty": "vide aujourd'hui",
		"fortnite.shop.more":  "+%d de plus",

		"reauth.revoked.title": "Le bot a perdu l'accès à votre chaîne",
		"reauth.revoked.body": "Twitch a révoqué l'autorisation du bot pour votre chaîne (cela arrive après un changement de mot de passe ou si l'application a été déconnectée). " +
			"Les réponses et alertes sont en pause. Connectez-vous sur " + DashboardURL + " et reconnectez votre compte Twitch pour rétablir le bot.",
		"reauth.revoked.chat": "Le bot a perdu son autorisation Twitch pour cette chaîne (changement de mot de passe ou déconnexion de l'application). " +
			"Connectez-vous sur " + DashboardURL + " pour le reconnecter.",

		"grant.dead.title": "Reconnectez votre compte Twitch",
		"grant.dead.body": "Quelques connexions de votre chaîne ont expiré. " +
			"Veuillez simplement vous rendre dans les paramètres et reconnecter votre compte pour régler le problème.",
		"grant.dead.chat": "Quelques connexions de votre chaîne ont expiré. " +
			"Veuillez simplement vous connecter au dashboard pour régler le problème : " + DashboardURL,
	},
}

// Missing reports the keys present in the default locale but absent from
// locale, so a parity test can fail on a half-translated addition instead of
// letting T silently fall back to English in a French streamer's chat.
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

// Locales lists every locale the catalog carries.
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
