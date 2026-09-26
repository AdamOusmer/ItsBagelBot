// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package schema

import (
	"context"
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

		field.Int64("state_revision").Default(1).Positive(),

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

// Hooks assigns revision increments in the same SQL mutation as every update.
// It covers bulk updates and transactional clients, not only publication paths.
func (User) Hooks() []ent.Hook {
	return []ent.Hook{func(next ent.Mutator) ent.Mutator {
		return ent.MutateFunc(func(ctx context.Context, m ent.Mutation) (ent.Value, error) {
			if m.Op().Is(ent.OpCreate) {
				if err := m.SetField("state_revision", int64(1)); err != nil {
					return nil, err
				}
			}
			if m.Op().Is(ent.OpUpdate | ent.OpUpdateOne) {
				if err := m.ResetField("state_revision"); err != nil {
					return nil, err
				}
				if err := m.AddField("state_revision", int64(1)); err != nil {
					return nil, err
				}
			}
			return next.Mutate(ctx, m)
		})
	}}
}
