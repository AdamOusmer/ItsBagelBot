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

// AuthorizationCode is a single-use desktop device authorization code, stored
// only as a hash. Reuse after consumption revokes whatever grant it produced.
type AuthorizationCode struct {
	ent.Schema
}

func (AuthorizationCode) Fields() []ent.Field {
	return []ent.Field{
		field.Bytes("code_hash").Unique().Sensitive().
			SchemaType(map[string]string{dialect.MySQL: "binary(32)"}),

		field.Uint64("user_id"),

		field.String("code_challenge").NotEmpty().MaxLen(128),
		field.String("dpop_jkt").Default("").MaxLen(43),
		field.String("redirect_uri").NotEmpty().MaxLen(255),
		field.String("device_name").Default("").MaxLen(64),
		field.Enum("platform").Values("macos", "windows", "linux"),

		field.Time("created_at").Default(time.Now).Immutable(),
		field.Time("expires_at"),
		field.Time("consumed_at").Optional().Nillable(),
		field.String("produced_grant_id").Optional().Nillable().MaxLen(36),
	}
}

func (AuthorizationCode) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("user", User.Type).Ref("authorization_codes").Field("user_id").Unique().Required(),
	}
}

func (AuthorizationCode) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("expires_at", "id"),
	}
}
