package schema

import (
	"context"
	"strings"
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

		// Alternate names the command also answers to. Stored as a JSON array so
		// adding or removing an alias never needs a column migration. The bot
		// resolves each alias to this command; the primary name always wins on a
		// collision.
		field.Strings("aliases").Optional(),

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

func (Commands) Hooks() []ent.Hook {
	return []ent.Hook{
		func(next ent.Mutator) ent.Mutator {
			return ent.MutateFunc(func(ctx context.Context, m ent.Mutation) (ent.Value, error) {
				if m.Op().Is(ent.OpCreate | ent.OpUpdateOne | ent.OpUpdate) {
					if name, exists := m.Field("name"); exists {
						if nameStr, ok := name.(string); ok {
							// Store the bare trigger: strip the leading "!" and
							// lower-case it. Chat carries the "!" to invoke; the
							// stored/looked-up key never does, so "!test" and
							// "test" are the same command.
							norm := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(nameStr), "!")))
							if err := m.SetField("name", norm); err != nil {
								return nil, err
							}
						}
					}
				}
				return next.Mutate(ctx, m)
			})
		},
	}
}
