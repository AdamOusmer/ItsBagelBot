// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// DeviceGrant is one desktop device authorization: identity, device_id for
// access-token payloads/revocation, and the current refresh-token family.
type DeviceGrant struct {
	ent.Schema
}

func (DeviceGrant) Fields() []ent.Field {
	return []ent.Field{
		field.String("grant_id").NotEmpty().Unique().Immutable().MaxLen(36),
		field.String("device_id").NotEmpty().Unique().Immutable().MaxLen(36),

		field.Uint64("user_id"),

		field.String("device_name").Default("").MaxLen(64),
		field.Enum("platform").Values("macos", "windows", "linux"),

		field.Bytes("refresh_hash").Unique().Sensitive().
			SchemaType(map[string]string{dialect.MySQL: "binary(32)"}),
		field.Bytes("prev_refresh_hash").Optional().Nillable().Sensitive().
			SchemaType(map[string]string{dialect.MySQL: "binary(32)"}),
		field.Time("prev_rotated_at").Optional().Nillable(),

		field.Time("refresh_sliding_expires_at"),
		field.Time("refresh_absolute_expires_at").Immutable(),

		field.Time("created_at").Default(time.Now).Immutable(),
		field.Time("last_used_at").Optional().Nillable(),

		field.Time("revoked_at").Optional().Nillable(),
		field.String("revoked_reason").Default(""),
	}
}

func (DeviceGrant) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("user", User.Type).Ref("device_grants").Field("user_id").Unique().Required(),
	}
}

func (DeviceGrant) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("prev_refresh_hash"),
		index.Fields("user_id", "revoked_at", "id"),
		index.Fields("revoked_at", "refresh_absolute_expires_at", "id"),
	}
}
