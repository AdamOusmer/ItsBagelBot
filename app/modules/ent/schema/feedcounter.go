package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
)

// FeedCounter is the personality module's permanent "feed the bagel" tally: a
// single global row (fixed id 1) whose count only ever goes up. It is
// deliberately not per-channel; there is one bagel and every channel feeds it.
// The daily count lives in sesame's valkey with a TTL; this row is the
// lifetime source of truth.
type FeedCounter struct {
	ent.Schema
}

// Fields of the FeedCounter.
func (FeedCounter) Fields() []ent.Field {
	return []ent.Field{
		// Fixed id: the repository always writes row 1, so the table holds
		// exactly one row.
		field.Int("id").Unique(),
		field.Uint64("count").Default(0),
	}
}
