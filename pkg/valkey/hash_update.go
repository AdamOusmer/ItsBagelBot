// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package valkey

import (
	"context"

	valkey_go "github.com/valkey-io/valkey-go"
)

// Membership adds Member to Key when Include, else removes it.
type Membership struct {
	Key     string
	Member  string
	Include bool
}

func (m Membership) command(client valkey_go.Client) valkey_go.Completed {
	if m.Include {
		return client.B().Sadd().Key(m.Key).Member(m.Member).Build()
	}
	return client.B().Srem().Key(m.Key).Member(m.Member).Build()
}

// HashFieldsUpdate writes Defaults only where a field is absent, then Fields unconditionally.
type HashFieldsUpdate struct {
	Key      string
	Defaults map[string]string
	Fields   map[string]string
}

// Apply runs the defaults, the fields, and every membership change in one pipeline.
func (u HashFieldsUpdate) Apply(ctx context.Context, client valkey_go.Client, memberships ...Membership) error {
	cmds := make([]valkey_go.Completed, 0, len(u.Defaults)+1+len(memberships))
	for field, value := range u.Defaults {
		cmds = append(cmds, client.B().Hsetnx().Key(u.Key).Field(field).Value(value).Build())
	}
	if len(u.Fields) > 0 {
		hset := client.B().Hset().Key(u.Key).FieldValue()
		for field, value := range u.Fields {
			hset = hset.FieldValue(field, value)
		}
		cmds = append(cmds, hset.Build())
	}
	for _, m := range memberships {
		cmds = append(cmds, m.command(client))
	}
	return firstError(client.DoMulti(ctx, cmds...))
}
