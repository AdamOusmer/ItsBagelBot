// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type SpotifyCredential struct {
	ent.Schema
}

func (SpotifyCredential) Fields() []ent.Field {
	return []ent.Field{
		field.Uint64("user_id").Immutable(),

		field.Bytes("token_enc").Sensitive().Optional(),

		field.String("client_id").Optional().Default(""),
		field.Bytes("client_secret_enc").Sensitive().Optional(),

		field.String("scopes").Optional().Default(""),

		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

func (SpotifyCredential) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("user_id").
			Unique(),
	}
}
