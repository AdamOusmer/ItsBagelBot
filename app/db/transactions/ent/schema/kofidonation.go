// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type KofiDonation struct{ ent.Schema }

func (KofiDonation) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").Unique().Immutable().NotEmpty().MaxLen(64),
		field.String("message_id").Immutable().NotEmpty().MaxLen(64),
		field.Enum("kind").Values("donation", "subscription", "commission", "shop_order").Immutable(),
		field.Int64("amount_cents").Immutable().NonNegative(),
		field.String("currency").Immutable().NotEmpty().MaxLen(3),
		field.Bool("subscription_payment").Immutable().Default(false),
		field.Bool("first_subscription_payment").Immutable().Default(false),
		field.String("tier_name").Optional().Immutable().MaxLen(100),
		field.Time("occurred_at").Immutable().SchemaType(map[string]string{dialect.MySQL: "datetime"}),
		field.Time("created_at").Default(time.Now).Immutable(),
	}
}

func (KofiDonation) Indexes() []ent.Index {
	return []ent.Index{index.Fields("occurred_at")}
}
