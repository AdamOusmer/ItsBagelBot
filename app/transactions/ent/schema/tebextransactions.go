package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// TebexTransactions holds the schema definition for the Tebex transactions
// entity. We deliberately store only the Tebex transaction ID and the owning
// user; the payment details stay on Tebex's side.
type TebexTransactions struct {
	ent.Schema
}

// Fields of the TebexTransactions.
func (TebexTransactions) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").Unique().Immutable().NotEmpty(), // Tebex transaction ID

		field.Uint64("user_id").Immutable(),

		field.Time("created_at").Default(time.Now).Immutable(),
	}
}

func (TebexTransactions) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("user_id"),
	}
}
