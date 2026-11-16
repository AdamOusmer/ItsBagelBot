// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package analytics

import (
	"context"
	"time"
)

const (
	pageSize  = 1000
	pagePause = 25 * time.Millisecond
)

func Pages[R, C any](ctx context.Context, fetch func(ctx context.Context, after *C, limit int) ([]R, error), cursor func(R) C, visit func(R)) error {
	var after *C
	for {
		rows, err := fetch(ctx, after, pageSize)
		if err != nil {
			return err
		}
		for _, row := range rows {
			visit(row)
		}
		if len(rows) < pageSize {
			return nil
		}
		next := cursor(rows[len(rows)-1])
		after = &next
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(pagePause):
		}
	}
}
