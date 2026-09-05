// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// GuildBinding is the durable half of the Discord guild -> Twitch broadcaster
// link. It used to live only as the Valkey key discord:guild:<id>, written by
// outgress on setup and read by engine on every gateway event; Valkey stays in
// front of this table as a read-through cache, but the row here is the truth.
//
// One broadcaster owns MANY guilds (decided 2026-09-05): a streamer with a
// main community and a mod-only server installs the bot in both, and the
// dashboard lists every one of them. Only the guild half is unique.
//
// Snowflakes are strings and the broadcaster id is a uint64, matching the rest
// of the fleet: Discord ids are opaque strings everywhere in the Go code and
// Twitch ids are numeric everywhere. Do not unify them.
type GuildBinding struct {
	ent.Schema
}

// Fields of the GuildBinding.
func (GuildBinding) Fields() []ent.Field {
	return []ent.Field{
		field.String("guild_id").MaxLen(20).NotEmpty().Immutable(),

		field.Uint64("broadcaster_id"),

		// The Discord user snowflake that ran the setup. Kept for the audit
		// trail only; nothing branches on it.
		field.String("installed_by").MaxLen(20).Optional().Default(""),

		field.Time("bound_at").Default(time.Now).Immutable(),

		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

// Indexes of the GuildBinding.
func (GuildBinding) Indexes() []ent.Index {
	return []ent.Index{
		// One binding per guild: this is what turns outgress's "already
		// linked to another Twitch channel" refusal from a read-then-write
		// race into a database invariant, so two concurrent setups cannot
		// both win.
		index.Fields("guild_id").Unique(),
		// NOT unique. It was, until the multi-guild model landed; a broadcaster
		// binding a second server used to fail on the duplicate key rather
		// than on any deliberate check. The index stays because
		// binding.list_by_broadcaster reads by it on every dashboard load.
		index.Fields("broadcaster_id"),
	}
}
