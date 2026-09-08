// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package i18n

import "go.uber.org/zap"

// WarnGaps logs one warning per supported locale that is missing keys, so a
// half-translated language shows up in the startup logs. Missing keys fall back
// to English at lookup time (T), so this never blocks startup; a declared
// locale with no catalog file yet reports its whole key set, capped for
// readability.
//
// It lives next to the catalog instead of in each service's main because
// sesame and outgress shipped byte-identical copies of it (comments included),
// and the next service that links the catalog would have made a third. The
// alternative — a shared startup package — was rejected: the only thing the
// copies had in common was this catalog, so this is where the one copy belongs.
func WarnGaps(log *zap.Logger) {
	for locale, missing := range Gaps() {
		if len(missing) == 0 {
			continue
		}
		log.Warn("i18n locale is missing keys; falling back to English",
			zap.String("locale", locale),
			zap.Int("missing_count", len(missing)),
			zap.Strings("missing_keys", capGapKeys(missing)))
	}
}

// capGapKeys bounds the key list logged for a locale gap so a single warning
// line stays readable when an entire catalog file is absent.
func capGapKeys(keys []string) []string {
	const maxKeys = 20
	if len(keys) > maxKeys {
		return keys[:maxKeys]
	}
	return keys
}
