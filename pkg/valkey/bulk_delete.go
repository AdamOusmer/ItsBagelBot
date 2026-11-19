// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package valkey

import (
	"context"
	"slices"

	valkey_go "github.com/valkey-io/valkey-go"
)

func DelKeys(ctx context.Context, client valkey_go.Client, keys ...string) error {
	if len(keys) == 0 {
		return nil
	}
	return client.Do(ctx, client.B().Del().Key(keys...).Build()).Error()
}

type ScanUnlink struct {
	Match  string
	Count  int64
	Filter func(string) bool
}

func (s ScanUnlink) Run(ctx context.Context, client valkey_go.Client) error {
	var cursor uint64
	for {
		page, err := client.Do(ctx, client.B().Scan().Cursor(cursor).Match(s.Match).Count(s.Count).Build()).AsScanEntry()
		if err != nil {
			return err
		}
		if err := UnlinkAll(ctx, client, s.keys(page.Elements)); err != nil {
			return err
		}
		if page.Cursor == 0 {
			return nil
		}
		cursor = page.Cursor
	}
}

func (s ScanUnlink) keys(keys []string) []string {
	if s.Filter == nil {
		return keys
	}
	return slices.DeleteFunc(keys, func(key string) bool { return !s.Filter(key) })
}
