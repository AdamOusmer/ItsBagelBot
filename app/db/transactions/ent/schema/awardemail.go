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

// AwardEmail is the durable local ledger for giveaway notification delivery.
type AwardEmail struct{ ent.Schema }

func (AwardEmail) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").Unique().Immutable().NotEmpty(),
		field.String("award_id").NotEmpty(),
		field.String("kind").NotEmpty(),
		field.String("template_version").NotEmpty(),
		field.String("delivery_key").NotEmpty(),
		field.Int("months").Positive(),
		field.Time("period_start").SchemaType(map[string]string{dialect.MySQL: "datetime(6)"}).Optional(),
		field.Time("period_end").SchemaType(map[string]string{dialect.MySQL: "datetime(6)"}).Optional(),
		field.Bool("subscriber").Default(false),
		field.Bool("billing_pending").Default(true),
		field.Bool("confirmation_queued").Default(false),
		field.String("state").Default("queued"),
		field.String("recipient_hash").Optional(),
		field.Text("content_json").Optional(),
		field.Time("first_attempt_at").Optional().Nillable(),
		field.String("provider_message_id").Optional(),
		field.Uint64("attempts").Default(0),
		field.String("error_category").Optional(),
		field.String("last_error").Optional().MaxLen(1000),
		field.Time("accepted_at").Optional(),
		field.Time("created_at").Default(time.Now).Immutable(),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

func (AwardEmail) Indexes() []ent.Index {
	return []ent.Index{index.Fields("award_id", "kind").Unique(), index.Fields("state"), index.Fields("delivery_key").Unique()}
}
