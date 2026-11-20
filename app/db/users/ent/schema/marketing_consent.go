// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package schema

import (
	"ItsBagelBot/internal/domain/consent"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type MarketingConsent struct {
	ent.Schema
}

func (MarketingConsent) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "marketing_consent"}}
}

func (MarketingConsent) Fields() []ent.Field {
	return []ent.Field{
		field.Uint64("user_id").Immutable(),
		field.Int64("account_created_at").Immutable(),
		field.Enum("status").Values(string(consent.Granted), string(consent.Withdrawn)).Immutable(),
		field.Enum("basis").Values("express").Default("express").Immutable(),
		field.Enum("source").Values(
			string(consent.Welcome), string(consent.Settings), string(consent.Prompt),
			string(consent.Consent), string(consent.PreferencePage), string(consent.OneClick),
			string(consent.Complaint), string(consent.Admin), string(consent.AddressChanged),
		).Immutable(),
		field.String("policy_version").NotEmpty().MaxLen(64).Immutable(),
		field.String("consent_text_key").NotEmpty().MaxLen(128).Immutable(),
		field.String("locale").NotEmpty().MaxLen(8).Immutable(),
		field.Bytes("address_hmac").Optional().Sensitive().Immutable(),
		field.Time("created_at").Immutable(),
	}
}

func (MarketingConsent) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("user", User.Type).Ref("marketing_consents").Field("user_id").Unique().Required().Immutable(),
	}
}

func (MarketingConsent) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("user_id", "account_created_at", "status", "id"),
	}
}
