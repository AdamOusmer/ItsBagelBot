package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// TebexWebhookEvents tracks webhook processing state without storing full
// payment payloads. Tebex remains the payment detail system of record.
type TebexWebhookEvents struct {
	ent.Schema
}

func (TebexWebhookEvents) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").Unique().Immutable().NotEmpty(), // Tebex webhook ID

		field.String("event_type").Immutable().NotEmpty(),

		field.Enum("status").
			Values("processed", "failed", "ignored", "validation").
			Default("processed"),

		field.String("transaction_id").Optional(),

		field.Uint64("user_id").Optional(),

		field.String("error").Optional().MaxLen(500),

		field.Time("created_at").Default(time.Now).Immutable(),

		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

func (TebexWebhookEvents) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("status"),
		index.Fields("event_type"),
		index.Fields("user_id"),
		index.Fields("transaction_id"),
	}
}
