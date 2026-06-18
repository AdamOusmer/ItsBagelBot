package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// Commands holds the schema definition for the custom chat commands entity.
// The user is referenced by its Twitch ID only; the users service owns the
// user record and schemas are isolated per service.
type Commands struct {
	ent.Schema
}

// Fields of the Commands.
func (Commands) Fields() []ent.Field {
	return []ent.Field{
		field.Uint64("user_id").Immutable(),

		field.String("name").NotEmpty(),

		field.String("response").NotEmpty(),

		field.Bool("is_active").Default(true),

		// When true, the command can only run while Twitch reports the
		// broadcaster's stream as online.
		field.Bool("stream_online_only").Default(false),

		// Minimum role allowed to run the command. One of: everyone, sub, vip,
		// mod, lead_mod, broadcaster. Validated at the trust boundary, stored as
		// a plain string so adding a tier never needs a column migration.
		field.String("perm").Default("everyone"),

		// Per-command cooldown in seconds; 0 means no cooldown.
		field.Uint("cooldown").Default(0),

		// When non-zero, only this Twitch user id may run the command (overrides
		// perm). 0 means the perm tier applies normally.
		field.Uint64("allowed_user_id").Default(0),

		field.Time("created_at").Default(time.Now),

		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

func (Commands) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("user_id", "name").
			Unique(),
	}
}
