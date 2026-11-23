// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"time"

	"ItsBagelBot/app/twitch/sesame/module"
)

func ClampRaffleDuration(d time.Duration) time.Duration {
	return max(minRaffleDuration, min(d, maxRaffleDuration))
}

type tokenExpansion struct {
	namespace module.Namespace
	text      string
	kv        []string
}

func expandTokens(locale module.Locale, e tokenExpansion) string {
	return module.KV(e.kv...).WithLocale(locale).WithNamespace(e.namespace).ExpandString(e.text)
}
