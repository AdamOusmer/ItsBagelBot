// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package live

import (
	"context"
	"strconv"
	"strings"

	pkgvalkey "ItsBagelBot/pkg/valkey"

	valkey_go "github.com/valkey-io/valkey-go"
)

// ScanCount is the SCAN page hint for the live-key walk. The timer reconciler
// and the admin directory both use it; a smaller hint only adds round trips.
const ScanCount int64 = 1000

// BroadcasterIDs returns every broadcaster whose live:<id> key exists.
// The walk is pkg/valkey.ScanAll: a scan that stops early is an error, not a
// short list, because a partial page would be read as everyone who is live.
// live:ver: and live:recheck: share the prefix and are skipped; their suffix
// is not a broadcaster id.
func BroadcasterIDs(ctx context.Context, client valkey_go.Client) ([]uint64, error) {
	var ids []uint64
	err := pkgvalkey.ScanAll(ctx, client, pkgvalkey.ScanQuery{
		Match: KeyPrefix + "*",
		Type:  "string",
		Count: ScanCount,
	}, func(keys []string) error {
		for _, key := range keys {
			if id, ok := BroadcasterID(key); ok {
				ids = append(ids, id)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return ids, nil
}

// BroadcasterID reports the id in a live:<id> key. Anything else under the
// prefix (the version key, the recheck guard) is not a broadcaster.
func BroadcasterID(key string) (uint64, bool) {
	rest, ok := strings.CutPrefix(key, KeyPrefix)
	if !ok {
		return 0, false
	}
	if !startsWithNonZeroDigit(rest) {
		return 0, false
	}
	return parseBroadcasterID(rest)
}

func startsWithNonZeroDigit(rest string) bool {
	if rest == "" {
		return false
	}
	if rest[0] < '1' {
		return false
	}
	return rest[0] <= '9'
}

func parseBroadcasterID(rest string) (uint64, bool) {
	id, err := strconv.ParseUint(rest, 10, 64)
	if err != nil {
		return 0, false
	}
	if id == 0 {
		return 0, false
	}
	return id, true
}
