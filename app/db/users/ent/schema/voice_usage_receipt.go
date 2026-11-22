// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package schema

import (
	usersrpc "ItsBagelBot/internal/domain/rpc/users"

	"entgo.io/ent"
	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type VoiceUsageReceipt struct{ ent.Schema }

func (VoiceUsageReceipt) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "voice_usage_receipt"}}
}

func (VoiceUsageReceipt) Fields() []ent.Field {
	return []ent.Field{
		field.String("request_id").NotEmpty().MaxLen(usersrpc.VoiceRequestIDMaxBytes).Immutable().
			SchemaType(map[string]string{dialect.MySQL: "varbinary(255)"}),
		field.Uint64("user_id").Immutable(),
		field.String("period").MaxLen(10).Immutable(),
		field.Int64("audio_tokens").NonNegative().Immutable(),
		field.Time("created_at").Immutable(),
		field.JSON("allowance", usersrpc.VoiceAllowance{}).Immutable(),
	}
}

func (VoiceUsageReceipt) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("user", User.Type).Ref("voice_usage_receipts").Field("user_id").Unique().Required().Immutable(),
	}
}

func (VoiceUsageReceipt) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("user_id", "request_id").Unique(),
		index.Fields("created_at"),
	}
}
