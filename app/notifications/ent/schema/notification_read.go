package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// NotificationRead holds the schema definition for the NotificationRead
// entity: one row per (notification, user) that has acknowledged it.
type NotificationRead struct {
	ent.Schema
}

func (NotificationRead) Fields() []ent.Field {
	return []ent.Field{

		field.Uint64("user_id"),

		field.Time("read_at").Default(time.Now).Immutable(),

		// Per-user visibility cutoff. Once passed the notification drops out of
		// this user's list even though the row (and the notification) still
		// exist. A full read sets a short cutoff; a dropdown "peek" sets a longer
		// reduced one. Nil means the read never lapses (legacy rows).
		field.Time("expires_at").Optional().Nillable(),
	}
}

func (NotificationRead) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("notification", Notification.Type).
			Ref("reads").
			Unique().
			Required(),
	}
}

func (NotificationRead) Indexes() []ent.Index {
	return []ent.Index{
		// One read row per (notification, user).
		index.Fields("user_id").
			Edges("notification").
			Unique(),
	}
}
