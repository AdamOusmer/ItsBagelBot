package schema

import (
	"ItsBagelBot/internal/domain/lifecycle"
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"time"
)

type LifecycleErasure struct{ ent.Schema }

func (LifecycleErasure) Fields() []ent.Field {
	return []ent.Field{
		field.Uint64("user_id"), field.Int64("account_created_at"),
		field.Time("deleted_at").Default(time.Now),
		field.JSON("lifecycle", &lifecycle.State{}).Optional(),
		field.Bool("notifications_done").Default(false), field.Bool("outgress_done").Default(false),
		field.Time("next_attempt_at").Default(time.Now), field.Time("completed_at").Optional().Nillable(),
	}
}
func (LifecycleErasure) Indexes() []ent.Index {
	return []ent.Index{index.Fields("user_id", "account_created_at").Unique(), index.Fields("completed_at", "next_attempt_at", "id")}
}
