// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type User struct {
	ent.Schema
}

func (User) Fields() []ent.Field {

	return []ent.Field{

		field.Uint64("id").Unique().Immutable(),

		field.String("username").NotEmpty(),

		field.String("display_name").Default("").MaxLen(64),

		field.String("email").NotEmpty().Unique().Sensitive(),

		field.Bytes("email_enc").Optional().Sensitive(),

		field.Bool("is_active").Default(true),

		field.Bool("banned").Default(false),

		field.Enum("status").
			Values("free", "paid", "vip").
			Default("free"),

		field.String("locale").Default("en").MaxLen(8),

		field.Bool("custom_cursor").Default(true),

		field.Bool("commands_page_hidden").Default(false),

		field.String("creator_code").Optional().Nillable().MaxLen(64),

		field.String("subscription_source").Default(""),
		field.Time("subscription_expires_at").Optional().Nillable(),
		field.String("subscription_ref").Optional().Nillable(),
		field.Bool("subscription_cancel_pending").Default(false),
		field.Time("billing_event_at").Optional().Nillable(),
		field.String("billing_event_id").Optional().Nillable(),

		field.Uint32("gifts_sent").Default(0),

		field.Bool("onboarded").Default(false),

		field.Bool("test_account").Default(false),

		field.Time("created_at").Default(time.Now),

		field.Time("updated_at").
			Default(time.Now).
			UpdateDefault(time.Now),
	}

}

func (User) Edges() []ent.Edge {

	return []ent.Edge{

		edge.To("tokens", Tokens.Type).
			Annotations(entsql.Annotation{
				OnDelete: entsql.Cascade,
			}),
		edge.To("premium_grants", PremiumGrant.Type).
			Annotations(entsql.Annotation{
				OnDelete: entsql.Cascade,
			}),
	}
}

func (User) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("status", "subscription_source", "subscription_expires_at"),

		// Must stay non-unique: a rename frees the old login while our row still carries it.
		index.Fields("username"),
	}
}
