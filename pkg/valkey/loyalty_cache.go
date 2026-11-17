// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package valkey

import (
	"context"
	"strconv"
	"strings"

	"ItsBagelBot/pkg/cache"
	valkey_go "github.com/valkey-io/valkey-go"
)

type LoyaltyCache struct{ Client valkey_go.Client }

func (c *LoyaltyCache) Drop(ctx context.Context, e cache.Event) error {
	if c == nil {
		return nil
	}
	if len(e.Keys) == 0 {
		return (ScanUnlink{Match: "loyal:*", Count: 500, Filter: loyaltyChannel(e.ID).matches}).Run(ctx, c.Client)
	}
	keys := make([]string, 0, len(e.Keys))
	for _, raw := range e.Keys {
		id, err := strconv.ParseUint(raw, 10, 64)
		if err == nil && id != 0 {
			keys = append(keys, cache.PairKey("loyal:bal:", e.ID, strconv.FormatUint(id, 10)))
		}
	}
	return UnlinkAll(ctx, c.Client, keys)
}

type loyaltyChannel uint64

func (id loyaltyChannel) matches(key string) bool {
	for _, prefix := range []string{"loyal:bal:", "loyal:cnt:c:", "loyal:cnt:v:"} {
		if strings.HasPrefix(key, cache.PairKey(prefix, uint64(id), "")) {
			return true
		}
	}
	return false
}
