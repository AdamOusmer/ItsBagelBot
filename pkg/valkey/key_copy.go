// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package valkey

import (
	"context"
	"time"

	valkey_go "github.com/valkey-io/valkey-go"
)

type KeyCopy struct {
	Source      string
	Destination string
	ExpireAt    time.Time
}

func (c KeyCopy) Replace(ctx context.Context, client valkey_go.Client) error {
	return client.Dedicated(func(dc valkey_go.DedicatedClient) error {
		results := dc.DoMulti(ctx,
			dc.B().Multi().Build(),
			dc.B().Copy().Source(c.Source).Destination(c.Destination).Replace().Build(),
			dc.B().Expireat().Key(c.Destination).Timestamp(c.ExpireAt.Unix()).Build(),
			dc.B().Exec().Build(),
		)
		if err := firstError(results); err != nil {
			return err
		}
		messages, err := results[len(results)-1].ToArray()
		if err != nil {
			return err
		}
		return firstMessageError(messages)
	})
}
