// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// FetchKey holds one broadcaster's third-party API key, sealed at rest with
// the commands service's own Tink AEAD keyset — the shape of modules'
// GoveeCredential. Plaintext never touches the database, logs or the
// projection; gossip receives it decrypted exactly once per fetch over the
// internal key RPC and never caches it. The user is referenced by Twitch id
// only, like every other schema in this service.
type FetchKey struct {
	ent.Schema
}

// Fields of the FetchKey.
func (FetchKey) Fields() []ent.Field {
	return []ent.Field{
		field.Uint64("user_id").Immutable(),

		// Author-chosen name the definition's key_label points at.
		field.String("label").NotEmpty().MaxLen(32),

		// Tink AEAD ciphertext of the API key. The associated data binds the
		// envelope to this user id AND label (fetchAAD), so an envelope copied
		// onto another row — even the same broadcaster's other label — fails
		// to open rather than leak.
		field.Bytes("key_enc").Sensitive(),

		// Last four characters of the plaintext, derived once at seal time so
		// the dashboard can show "sk-…a1b2" forever without any decrypt path
		// existing. Display-only: custody code must never treat it as secret
		// material, and nothing else ever reads it.
		field.String("last4").MaxLen(4),

		field.Time("created_at").Default(time.Now),

		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

func (FetchKey) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("user_id", "label").
			Unique(),
	}
}
