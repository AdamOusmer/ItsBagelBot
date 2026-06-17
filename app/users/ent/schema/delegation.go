package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// Delegation is a single-use authorization link: a dashboard owner grants
// another Twitch user scoped access (a subset of dashboard sections) to their
// dashboard. The link is consumed exactly once on the invitee's login; after
// that consumed_at is set and the token can never be reused.
type Delegation struct {
	ent.Schema
}

func (Delegation) Fields() []ent.Field {
	return []ent.Field{
		field.String("token").NotEmpty().Unique(),

		field.Uint64("owner_id"),
		field.String("owner_login"),

		// Granted dashboard sections, e.g. ["commands","modules"].
		field.Strings("sections"),

		field.Uint64("delegate_id").Optional().Default(0),
		field.String("delegate_login").Optional(),

		field.Time("consumed_at").Optional().Nillable(),

		field.Time("created_at").Default(time.Now).Immutable(),

		field.Time("expires_at").Optional().Nillable(),
	}
}

func (Delegation) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("token").Unique(),
		index.Fields("owner_id"),
		index.Fields("delegate_id"),
	}
}
