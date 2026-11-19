// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package worker

import (
	"strings"

	"ItsBagelBot/pkg/cache"
)

func loginKeys(e cache.Event) []string {
	keys := make([]string, 0, len(e.Keys))
	for _, login := range e.Keys {
		keys = append(keys, "login:"+strings.ToLower(strings.TrimSpace(login)))
	}
	return keys
}
