// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type AdminAudit struct {
	ent.Schema
}

func (AdminAudit) Fields() []ent.Field {
	return []ent.Field{
		field.Uint64("actor_id"),
		field.String("actor_login").NotEmpty(),

		field.String("action").NotEmpty(),
		field.String("target").Optional(),
		field.String("detail").Optional(),
		field.Bool("ok").Default(true),
		field.String("error").Optional(),

		field.Time("created_at").Default(time.Now),
	}
}

func (AdminAudit) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("created_at", "id"),
		index.Fields("actor_id", "created_at", "id"),
	}
}
