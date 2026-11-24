// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package schema

import (
	"time"

	"ItsBagelBot/internal/domain/consent"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type MarketingCampaign struct {
	ent.Schema
}

func (MarketingCampaign) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "marketing_campaign"}}
}

func (MarketingCampaign) Fields() []ent.Field {
	return []ent.Field{
		field.String("name").NotEmpty().MaxLen(120),
		field.Enum("status").Values(
			string(consent.CampaignDraft), string(consent.CampaignApproved), string(consent.CampaignSending),
			string(consent.CampaignPaused), string(consent.CampaignDone),
		).Default(string(consent.CampaignDraft)),
		field.JSON("content", map[string]consent.CampaignContent{}),
		field.String("created_by").NotEmpty().MaxLen(32),
		field.Time("created_at").Default(time.Now).Immutable(),
		field.String("approved_by").Optional().MaxLen(32),
		field.Time("approved_at").Optional().Nillable(),
		field.Int("revision").Default(1),
		field.Uint64("audience_cursor").Default(0),
		field.String("lease_holder").Optional().MaxLen(64),
		field.Time("lease_until").Optional().Nillable(),
	}
}

func (MarketingCampaign) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("sends", MarketingSend.Type).Annotations(entsql.OnDelete(entsql.Cascade)),
	}
}

func (MarketingCampaign) Indexes() []ent.Index {
	return []ent.Index{index.Fields("status", "id")}
}
