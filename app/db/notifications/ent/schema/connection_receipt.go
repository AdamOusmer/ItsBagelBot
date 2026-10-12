package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type ConnectionReceipt struct{ ent.Schema }

func (ConnectionReceipt) Fields() []ent.Field {
	return []ent.Field{
		field.Uint64("account_id"), field.String("incarnation").NotEmpty(), field.String("incident_id").NotEmpty(),
		field.String("outcome").NotEmpty(), field.Int("notification_id").Optional().Nillable(),
		field.Time("delivery_deadline"), field.Time("expires_at"), field.Time("retain_until"),
	}
}
func (ConnectionReceipt) Indexes() []ent.Index {
	return []ent.Index{index.Fields("account_id", "incarnation", "incident_id").Unique(), index.Fields("retain_until", "id")}
}
