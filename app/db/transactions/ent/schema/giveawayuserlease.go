// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"time"
)

// GiveawayUserLease serializes fulfillment across campaigns for one account.
type GiveawayUserLease struct{ ent.Schema }

func (GiveawayUserLease) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").Unique().Immutable().NotEmpty(),
		field.Uint64("user_id").Positive(),
		field.String("owner").NotEmpty(),
		field.Time("lease_until").SchemaType(map[string]string{dialect.MySQL: "datetime(6)"}),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}
func (GiveawayUserLease) Indexes() []ent.Index {
	return []ent.Index{index.Fields("user_id").Unique(), index.Fields("lease_until")}
}
