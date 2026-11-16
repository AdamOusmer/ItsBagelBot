// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package valkey

import (
	"context"
	"strconv"
	"time"

	valkeygo "github.com/valkey-io/valkey-go"
)

type IntHashSnapshot struct {
	Key      string
	Values   map[string]int64
	ExpireAt time.Time
}

func (s IntHashSnapshot) Replace(ctx context.Context, client valkeygo.Client) error {
	return client.Dedicated(func(dc valkeygo.DedicatedClient) error {
		cmds := valkeygo.Commands{dc.B().Multi().Build(), dc.B().Del().Key(s.Key).Build()}
		if len(s.Values) > 0 {
			set := dc.B().Hset().Key(s.Key).FieldValue()
			for field, value := range s.Values {
				set = set.FieldValue(field, strconv.FormatInt(value, 10))
			}
			cmds = append(cmds, set.Build())
		}
		cmds = append(cmds, dc.B().Expireat().Key(s.Key).Timestamp(s.ExpireAt.Unix()).Build(), dc.B().Exec().Build())
		results := dc.DoMulti(ctx, cmds...)
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
