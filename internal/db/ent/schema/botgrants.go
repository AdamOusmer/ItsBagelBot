package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// BotGrants holds the schema definition for the BotGrants entity.
type BotGrants struct {
	ent.Schema
}

// Fields of the BotGrants.
func (BotGrants) Fields() []ent.Field {
	return []ent.Field{
		field.String("broadcaster_user_id").
			Unique().
			NotEmpty(),

		field.String("scopes").
			NotEmpty(),

		field.Bytes("refresh_token_enc").
			NotEmpty(),

		field.Time("created_at").
			Default(time.Now),

		field.Time("updated_at").
			Default(time.Now).
			UpdateDefault(time.Now),
	}
}

// Edges of the BotGrants.
func (BotGrants) Edges() []ent.Edge {
	return nil
}

// Indexes of the BotGrants.
func (BotGrants) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("broadcaster_user_id"),
	}
}
