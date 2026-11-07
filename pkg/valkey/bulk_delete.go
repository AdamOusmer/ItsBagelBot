// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package valkey

import (
	"context"

	valkey_go "github.com/valkey-io/valkey-go"
)

// DelKeys deletes every key named, in one call.
func DelKeys(ctx context.Context, client valkey_go.Client, keys ...string) error {
	if len(keys) == 0 {
		return nil
	}
	return client.Do(ctx, client.B().Del().Key(keys...).Build()).Error()
}

// ScanUnlink unlinks every key matching Match, paging through SCAN until the cursor returns to zero.
type ScanUnlink struct {
	Match string
	Count int64
}

func (s ScanUnlink) Run(ctx context.Context, client valkey_go.Client) error {
	var cursor uint64
	for {
		page, err := client.Do(ctx, client.B().Scan().Cursor(cursor).Match(s.Match).Count(s.Count).Build()).AsScanEntry()
		if err != nil {
			return err
		}
		if err := UnlinkAll(ctx, client, page.Elements); err != nil {
			return err
		}
		if page.Cursor == 0 {
			return nil
		}
		cursor = page.Cursor
	}
}
