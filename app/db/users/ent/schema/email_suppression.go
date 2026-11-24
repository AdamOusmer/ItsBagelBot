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
	"entgo.io/ent/schema/field"
)

type EmailSuppression struct {
	ent.Schema
}

func (EmailSuppression) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "email_suppression"}}
}

func (EmailSuppression) Fields() []ent.Field {
	return []ent.Field{
		field.Bytes("address_hmac").NotEmpty().Unique().Immutable().Sensitive().
			SchemaType(map[string]string{dialect.MySQL: "varbinary(80)"}),
		field.Enum("reason").Values(
			string(consent.SuppressUnsubscribe), string(consent.SuppressComplaint), string(consent.SuppressHardBounce),
		),
		field.Time("created_at").Default(time.Now).Immutable(),
	}
}
