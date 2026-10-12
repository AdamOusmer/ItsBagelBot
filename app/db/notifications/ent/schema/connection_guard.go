package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type ConnectionGuard struct{ ent.Schema }

func (ConnectionGuard) Fields() []ent.Field {
	return []ent.Field{
		field.Uint64("account_id"), field.String("incarnation").NotEmpty(), field.Time("last_created_at").Optional().Nillable(), field.Time("retain_until"), field.Time("erased_at").Optional().Nillable(), field.Bool("erased").Default(false),
	}
}
func (ConnectionGuard) Indexes() []ent.Index {
	return []ent.Index{index.Fields("account_id", "incarnation").Unique(), index.Fields("retain_until", "id")}
}
