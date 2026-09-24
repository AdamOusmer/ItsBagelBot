// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"context"
	"time"
)

func (s *Store) AgeLastDaily(ctx context.Context, id int, d time.Duration) error {
	row, err := s.client.MemberXP.Get(ctx, id)
	if err != nil {
		return err
	}
	if row.LastDaily == nil {
		return ErrNotFound
	}
	return s.client.MemberXP.UpdateOneID(id).SetLastDaily(row.LastDaily.Add(-d)).Exec(ctx)
}
