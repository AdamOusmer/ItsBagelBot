// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"context"
	"time"
)

// AgeLastDaily backdates one member row's last_daily by d. It exists only in
// the test binary (export_test.go): the alternative for exercising the daily
// window's far edge is a test that waits 24 hours, and injecting a clock into
// the repository would put a seam in production code that nothing but the test
// would ever move.
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
