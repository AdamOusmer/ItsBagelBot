package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// GoveeCredential holds one broadcaster's Govee API key, sealed at rest. The
// key is a third-party secret, so it never lands in the projected module
// configs blob (which is cached and fanned out in cleartext); it lives here as
// Tink AEAD ciphertext instead, and only the gateway ever receives it
// decrypted, over an internal RPC. The user is referenced by Twitch id only,
// like every other schema in this service.
type GoveeCredential struct {
	ent.Schema
}

// Fields of the GoveeCredential.
func (GoveeCredential) Fields() []ent.Field {
	return []ent.Field{
		field.Uint64("user_id").Immutable(),

		// Tink AEAD ciphertext of the Govee API key. Plaintext never touches
		// the database or logs; the associated data binds the ciphertext to the
		// owning user id so an envelope copied onto another row fails to open.
		field.Bytes("key_enc").Sensitive(),

		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

func (GoveeCredential) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("user_id").
			Unique(),
	}
}
