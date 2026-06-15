package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// AdminUser is the staff allowlist for the admin console. Membership is the
// authorization boundary on top of the tailnet: a Twitch sign-in only yields a
// session if the subject id appears here and is active. Three-tier hierarchy:
//
//	moderator — operates the user console; cannot see/manage staff.
//	admin     — moderator + manages staff (mods + admins), but cannot touch owner.
//	owner      — full power, including managing admins and other owners.
//
// Owners are seeded from OWNER_BOOTSTRAP_IDS (and admins from ADMIN_BOOTSTRAP_IDS)
// at boot so a fresh DB is never locked out.
type AdminUser struct {
	ent.Schema
}

func (AdminUser) Fields() []ent.Field {
	return []ent.Field{
		// Twitch user id (the OAuth subject). Immutable natural key.
		field.Uint64("id").Unique().Immutable(),

		field.String("login").NotEmpty(),

		field.String("display_name").NotEmpty(),

		// Staff tier. See type doc for the capability ladder.
		field.Enum("role").
			Values("moderator", "admin", "owner").
			Default("moderator"),

		field.Bool("active").Default(true),

		// Twitch id of the admin who added this one (0 for bootstrap seeds).
		field.Uint64("added_by").Default(0),

		field.Time("created_at").Default(time.Now),

		field.Time("updated_at").
			Default(time.Now).
			UpdateDefault(time.Now),
	}
}

func (AdminUser) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("active"),
	}
}
