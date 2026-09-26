// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
package projection

import (
	"context"

	"ItsBagelBot/internal/watchtime"
)

func (v *Store) RestoreAccount(ctx context.Context, id uint64, createdAt int64) (bool, error) {
	return watchtime.NewStore(v.primary).RestoreAccount(ctx, id, createdAt)
}
func (v *Store) DeleteAccount(ctx context.Context, id uint64, createdAt int64) (bool, error) {
	return watchtime.NewStore(v.primary).DeleteAccount(ctx, id, createdAt)
}
