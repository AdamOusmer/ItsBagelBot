// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type Tokens struct {
	ent.Schema
}

func (Tokens) Fields() []ent.Field {
	return []ent.Field{

		field.Enum("type").
			Values("access_token", "user_token").
			Default("access_token"),

		// Tink AEAD ciphertext. Plaintext tokens never touch the database;
		// the associated data binds the ciphertext to the owning user.
		field.Bytes("token").Sensitive(),

		field.Bytes("refresh_token").Optional().Sensitive(),

		field.Enum("platform").
			Values("twitch").
			Default("twitch"),

		// AccessTokenExpiresAt is Twitch's expires_in (an OAuth response
		// field, ~4h for a user token) converted to an absolute UTC time by
		// the writer. It is a timestamp, not a secret, so unlike token/
		// refresh_token above it is stored in the clear -- nothing here
		// reveals the token itself.
		//
		// Optional/nillable and must stay that way: rows written before this
		// field existed, and rows written by callers that don't know the
		// expiry (the admin token-set and dashboard OAuth-callback paths),
		// have no value here. A nil value means "expiry unknown" and every
		// reader MUST treat that as "assume expired", never as "valid
		// forever" -- see StoredTokenIO.Load in
		// app/twitch/outgress/internal/twitch/token.go, the one path that trusts
		// this column to skip a Twitch mint.
		field.Time("access_token_expires_at").
			Optional().
			Nillable(),
	}
}

func (Tokens) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("user", User.Type).
			Ref("tokens").
			Unique().
			Required(),
	}
}

func (Tokens) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("type", "platform").
			Edges("user").
			Unique(),
	}
}
