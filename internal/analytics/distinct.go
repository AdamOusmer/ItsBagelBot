// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package analytics

import (
	"context"

	catalog "ItsBagelBot/internal/domain/analytics"
	pkgvalkey "ItsBagelBot/pkg/valkey"
	"github.com/valkey-io/valkey-go"
)

func DistinctCounts(ctx context.Context, client valkey.Client, keys []string) (map[string]int64, error) {
	groups := distinctGroups(keys)
	counts := make(map[string]int64, len(groups))
	for field, group := range groups {
		n, err := group.Count(ctx, client)
		if err != nil {
			return nil, err
		}
		counts[field] = n
	}
	return counts, nil
}

func distinctGroups(keys []string) map[string]pkgvalkey.DistinctUnion {
	groups := map[string]pkgvalkey.DistinctUnion{}
	for _, key := range keys {
		ref, ok := catalog.ParseDistinctKey(key)
		if !ok {
			continue
		}
		group := groups[ref.Field]
		if ref.Legacy {
			group.Sketch = key
		} else {
			group.Set = key
		}
		groups[ref.Field] = group
	}
	return groups
}
