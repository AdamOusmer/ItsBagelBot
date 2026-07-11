package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// Quote holds the schema definition for the channel quotes entity (the quotes
// module's rows). Like Modules, the broadcaster is referenced by Twitch id
// only; the users service owns the user record and schemas are isolated per
// service.
type Quote struct {
	ent.Schema
}

// Fields of the Quote.
func (Quote) Fields() []ent.Field {
	return []ent.Field{
		field.Uint64("user_id").Immutable(),

		// Channel-local quote number, assigned max+1 on save. Removing a quote
		// leaves a hole so the numbers chat already knows keep pointing at the
		// same quote forever.
		field.Uint64("number").Immutable(),

		// The quote body as saved from chat. Sized under the 500-character chat
		// message cap so the "Quote #N: text (date)" readout always fits one
		// message (see repository.QuoteTextMaxLen for the enforced bound).
		field.String("text").NotEmpty().MaxLen(450),

		// Login of the moderator who saved it; audit only, never displayed.
		field.String("added_by").Default("").MaxLen(64),

		field.Time("created_at").Default(time.Now).Immutable(),
	}
}

func (Quote) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("user_id", "number").
			Unique(),
	}
}
