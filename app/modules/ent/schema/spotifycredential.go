// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// SpotifyCredential holds one broadcaster's Spotify OAuth refresh token,
// sealed at rest — the spotify twin of GoveeCredential. The token is a
// third-party secret, so it never lands in the projected module configs blob
// (which is cached and fanned out in cleartext); it lives here as Tink AEAD
// ciphertext instead, and only gossip ever receives it decrypted, over an
// internal RPC, to exchange against accounts.spotify.com. The user is
// referenced by Twitch id only, like every other schema in this service.
type SpotifyCredential struct {
	ent.Schema
}

// Fields of the SpotifyCredential.
func (SpotifyCredential) Fields() []ent.Field {
	return []ent.Field{
		field.Uint64("user_id").Immutable(),

		// Tink AEAD ciphertext of the OAuth refresh token. Plaintext never
		// touches the database or logs; the associated data binds the
		// ciphertext to the owning user id so an envelope copied onto another
		// row fails to open.
		field.Bytes("token_enc").Sensitive(),

		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

func (SpotifyCredential) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("user_id").
			Unique(),
	}
}
