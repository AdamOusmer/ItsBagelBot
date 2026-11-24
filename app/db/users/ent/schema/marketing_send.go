// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package schema

import (
	"time"

	"ItsBagelBot/internal/domain/consent"

	"entgo.io/ent"
	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type MarketingSend struct {
	ent.Schema
}

func (MarketingSend) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "marketing_send"}}
}

func (MarketingSend) Fields() []ent.Field {
	return []ent.Field{
		field.Int("campaign_id").Immutable(),
		field.Uint64("user_id").Immutable(),
		field.Bytes("address_hmac").NotEmpty().Immutable().Sensitive().
			SchemaType(map[string]string{dialect.MySQL: "varbinary(80)"}),
		field.Enum("status").Values(
			string(consent.SendQueued), string(consent.SendSent), string(consent.SendDelivered),
			string(consent.SendBounced), string(consent.SendComplained), string(consent.SendSkipped),
			string(consent.SendFailed),
		).Default(string(consent.SendQueued)),
		field.Int("attempts").Default(0),
		field.String("resend_id").Optional().MaxLen(64),
		field.String("idempotency_key").NotEmpty().Immutable().MaxLen(80),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

func (MarketingSend) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("campaign", MarketingCampaign.Type).Ref("sends").Field("campaign_id").Unique().Required().Immutable(),
		edge.From("user", User.Type).Ref("marketing_sends").Field("user_id").Unique().Required().Immutable(),
	}
}

func (MarketingSend) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("campaign_id", "address_hmac").Unique().
			Annotations(entsql.PrefixColumn("address_hmac", 40)),
		index.Fields("campaign_id", "user_id").Unique(),
		index.Fields("resend_id"),
		index.Fields("campaign_id", "status"),
	}
}
