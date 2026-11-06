// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package valkey

import (
	"context"

	valkey_go "github.com/valkey-io/valkey-go"
)

// ScanQuery bounds a SCAN page by key pattern, value type, and page size hint.
type ScanQuery struct {
	Match string
	Type  string
	Count int64
}

// ScanPage reads one SCAN page, for a caller that persists the cursor itself between calls.
func ScanPage(ctx context.Context, client valkey_go.Client, cursor uint64, q ScanQuery) (valkey_go.ScanEntry, error) {
	return client.Do(ctx, client.B().Scan().Cursor(cursor).Match(q.Match).Count(q.Count).Type(q.Type).Build()).AsScanEntry()
}

// ScanAll pages through SCAN until the cursor returns to zero, calling visit with each page's keys.
func ScanAll(ctx context.Context, client valkey_go.Client, q ScanQuery, visit func([]string) error) error {
	var cursor uint64
	for {
		page, err := ScanPage(ctx, client, cursor, q)
		if err != nil {
			return err
		}
		if err := visit(page.Elements); err != nil {
			return err
		}
		if page.Cursor == 0 {
			return nil
		}
		cursor = page.Cursor
	}
}
