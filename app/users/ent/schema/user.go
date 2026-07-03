package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
)

// User holds the schema definition for the User entity.
type User struct {
	ent.Schema
}

// Fields of the User.
func (User) Fields() []ent.Field {

	return []ent.Field{

		field.Uint64("id").Unique().Immutable(), // This is the primary key -- Getting it from Twitch User ID

		field.String("username").NotEmpty(),

		field.String("email").NotEmpty().Unique().Sensitive(),

		// Real contact email captured at Twitch login (user:read:email scope),
		// stored as a Tink AEAD envelope bound to the user id (AAD) so a
		// database leak never exposes addresses. Absent until the user's next
		// login. Deleted with the row, so account erasure covers it. The
		// legacy "email" column above stays a synthetic placeholder.
		field.Bytes("email_enc").Optional().Sensitive(),

		field.Bool("is_active").Default(true),

		field.Bool("banned").Default(false),

		field.Enum("status").
			Values("free", "paid", "vip"). // vip is a permanent paid tier
			Default("free"),

		// UI language preference for the console. Chosen at onboarding, editable
		// in settings, and mirrored into a cookie so the SSR render is fast. A
		// plain string (not an enum) so shipping a new locale needs no schema
		// migration; the console validates the value against its locale set.
		field.String("locale").Default("en").MaxLen(8),

		// Billing ownership is deliberately stored with the user tier. This lets
		// webhook retries apply idempotently and prevents a Tebex cancellation
		// from revoking a staff grant or permanent VIP status.
		field.String("subscription_source").Default(""),
		field.Time("subscription_expires_at").Optional().Nillable(),
		field.String("subscription_ref").Optional().Nillable(),
		field.Bool("subscription_cancel_pending").Default(false),
		field.Time("billing_event_at").Optional().Nillable(),
		field.String("billing_event_id").Optional().Nillable(),

		// Number of premium gifts this user has paid for (as the gifter). Bumped
		// once per gift when the payment lands, on the idempotent billing-apply
		// path so Tebex webhook retries never double-count. Existing rows get 0
		// on migration.
		field.Uint32("gifts_sent").Default(0),

		field.Time("created_at").Default(time.Now),

		field.Time("updated_at").
			Default(time.Now).
			UpdateDefault(time.Now),
	}

}

// Edges of the User.
func (User) Edges() []ent.Edge {

	return []ent.Edge{

		edge.To("tokens", Tokens.Type).
			Annotations(entsql.Annotation{
				OnDelete: entsql.Cascade,
			}),
	}
}

func (User) Indexes() []ent.Index {
	return nil
}
