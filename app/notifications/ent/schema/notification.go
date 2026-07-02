package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// Notification holds the schema definition for the Notification entity.
type Notification struct {
	ent.Schema
}

func (Notification) Fields() []ent.Field {
	return []ent.Field{

		field.Enum("scope").
			Values("broadcast", "direct"),

		// Unset for scope=broadcast; the recipient of a scope=direct notification.
		field.Uint64("target_user_id").Optional().Nillable(),

		field.String("title").NotEmpty(),

		field.Text("body").NotEmpty(),

		field.Enum("level").
			Values("info", "success", "warning", "critical").
			Default("info"),

		field.Uint64("created_by"),

		field.String("created_by_login").NotEmpty(),

		// Stable across all deliveries of one admin RPC. Nullable so existing
		// rows migrate cleanly; every new admin send supplies a value.
		field.String("request_id").Optional().Nillable().Unique().Immutable(),

		field.Time("created_at").Default(time.Now).Immutable(),

		// Unset means the notification never expires.
		field.Time("expires_at").Optional().Nillable(),
	}
}

func (Notification) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("reads", NotificationRead.Type).
			Annotations(entsql.Annotation{OnDelete: entsql.Cascade}),
	}
}

func (Notification) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("scope", "target_user_id"),
		index.Fields("created_at"),
	}
}
