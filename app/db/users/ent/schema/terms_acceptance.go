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
)

type TermsAcceptance struct {
	ent.Schema
}

func (TermsAcceptance) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "terms_acceptance"}}
}

func (TermsAcceptance) Fields() []ent.Field {
	return []ent.Field{
		field.Uint64("user_id").Immutable(),
		field.Int64("account_created_at").Immutable(),
		field.String("terms_version").NotEmpty().MaxLen(64).Immutable(),
		field.Time("accepted_at").Immutable(),
		field.Enum("source").Values(string(consent.Welcome), string(consent.Consent)).Immutable(),
		field.String("locale").NotEmpty().MaxLen(8).Immutable(),
		field.String("request_id").NotEmpty().MaxLen(128).Unique().Immutable(),
	}
}

func (TermsAcceptance) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("user", User.Type).Ref("terms_acceptances").Field("user_id").Unique().Required().Immutable(),
	}
}
