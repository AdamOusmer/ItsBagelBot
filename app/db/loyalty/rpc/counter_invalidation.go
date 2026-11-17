// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"ItsBagelBot/internal/domain/invalidate"
	"ItsBagelBot/pkg/tmpl"
)

func (l *loyaltyRPC) counterChanged(userID uint64, names ...string) {
	if l.invalidations == nil {
		return
	}
	keys := make([]string, len(names))
	for i, name := range names {
		keys[i] = tmpl.NormalizeName(name)
	}
	_ = l.invalidations.Publish(invalidate.ScopeLoyaltyCounters, userID, keys...)
}
