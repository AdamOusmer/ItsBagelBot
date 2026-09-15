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

// GiveawayOutbox makes fulfillment and notification intent survive process,
// NATS, and browser failures. Workers claim rows with a lease and retry the
// same event identity.
type GiveawayOutbox struct{ ent.Schema }

func (GiveawayOutbox) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").Unique().Immutable().NotEmpty(),
		field.String("aggregate_id").NotEmpty(),
		field.String("event_type").NotEmpty(),
		field.String("payload_json").NotEmpty(),
		field.String("state").Default("queued"),
		field.Uint64("attempts").Default(0),
		field.String("last_error").Optional().MaxLen(1000),
		field.Time("lease_until").Optional().SchemaType(map[string]string{dialect.MySQL: "datetime(6)"}),
		field.String("lease_owner").Optional(),
		field.Time("next_attempt_at").Optional().SchemaType(map[string]string{dialect.MySQL: "datetime(6)"}),
		field.Time("created_at").Default(time.Now).Immutable(),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

func (GiveawayOutbox) Indexes() []ent.Index {
	return []ent.Index{index.Fields("aggregate_id", "event_type").Unique(), index.Fields("state", "lease_until")}
}
