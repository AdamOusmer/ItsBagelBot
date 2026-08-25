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
		// Optional because the two halves of a Spotify setup are written by
		// two different flows: pasting the app credentials creates the row,
		// finishing the OAuth round trip fills this in (and vice versa — a
		// broadcaster who re-pastes credentials keeps their existing grant
		// until it stops working).
		field.Bytes("token_enc").Sensitive().Optional(),

		// The broadcaster's OWN Spotify application. There is no fleet-wide
		// app any more: each broadcaster registers one and pastes its
		// credentials into the console. The client id is public by
		// construction (it rides the authorize URL through the browser) so it
		// is stored in the clear and echoed back to the console; the secret
		// gets the same Tink AEAD sealing as the token, under its own AAD
		// label so a ciphertext cannot be swapped between the two columns.
		field.String("client_id").Optional().Default(""),
		field.Bytes("client_secret_enc").Sensitive().Optional(),

		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

func (SpotifyCredential) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("user_id").
			Unique(),
	}
}
