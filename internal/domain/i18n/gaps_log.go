// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package i18n

import "go.uber.org/zap"

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

func capGapKeys(keys []string) []string {
	const maxKeys = 20
	if len(keys) > maxKeys {
		return keys[:maxKeys]
	}
	return keys
}
