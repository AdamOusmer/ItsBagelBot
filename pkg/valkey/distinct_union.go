// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package valkey

import (
	"context"

	"github.com/google/uuid"
	valkey_go "github.com/valkey-io/valkey-go"
)

type DistinctUnion struct {
	Sketch string
	Set    string
}

func (u DistinctUnion) Count(ctx context.Context, client valkey_go.Client) (int64, error) {
	if u.Sketch == "" {
		return SetCard(ctx, client, u.Set)
	}
	if u.Set == "" {
		return HyperLogLogCount(ctx, client, u.Sketch)
	}
	members, err := SetMembers(ctx, client, u.Set)
	if err != nil {
		return 0, err
	}
	temporary := u.Sketch + ":union:" + uuid.NewString()
	var count int64
	err = client.Dedicated(func(dc valkey_go.DedicatedClient) error {
		commands := valkey_go.Commands{
			dc.B().Multi().Build(),
			dc.B().Pfmerge().Destkey(temporary).Sourcekey(u.Sketch).Build(),
			dc.B().Expire().Key(temporary).Seconds(60).Build(),
		}
		if len(members) > 0 {
			commands = append(commands, dc.B().Pfadd().Key(temporary).Element(members...).Build())
		}
		commands = append(commands, dc.B().Pfcount().Key(temporary).Build(), dc.B().Del().Key(temporary).Build(), dc.B().Exec().Build())
		results := dc.DoMulti(ctx, commands...)
		if err := firstError(results); err != nil {
			return err
		}
		messages, err := results[len(results)-1].ToArray()
		if err != nil {
			return err
		}
		if err := firstMessageError(messages); err != nil {
			return err
		}
		count, err = messages[len(messages)-2].AsInt64()
		return err
	})
	return count, err
}
