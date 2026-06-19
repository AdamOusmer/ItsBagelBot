package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// AdminAudit is the append-only "who did what" log for the admin console. Every
// mutating action (status/active change, reset, token clear, delete, admin
// management) writes one row so operator actions are attributable after the
// fact. Read paths never write here.
type AdminAudit struct {
	ent.Schema
}

func (AdminAudit) Fields() []ent.Field {
	return []ent.Field{
		// Auto-increment primary key (ent default int id).

		// Who acted: Twitch id + login captured at action time (denormalized so
		// the log survives an admin being removed).
		field.Uint64("actor_id"),
		field.String("actor_login").NotEmpty(),

		// Verb, e.g. "set_status", "set_active", "reset", "clear_token",
		// "delete", "restart", "admin_add", "admin_remove".
		field.String("action").NotEmpty(),

		// Target of the action (user id/username), optional.
		field.String("target").Optional(),

		// Free-form detail (e.g. the new status), optional.
		field.String("detail").Optional(),

		// Outcome.
		field.Bool("ok").Default(true),
		field.String("error").Optional(),

		field.Time("created_at").Default(time.Now),
	}
}

func (AdminAudit) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("created_at", "id"),
		index.Fields("actor_id", "created_at", "id"),
	}
}
