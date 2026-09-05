// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"

	ddiscord "ItsBagelBot/internal/domain/discord"
)

// GuildConfig is one guild's Discord settings: the channel ids, role ids and
// feature toggles the engine reads on every event.
//
// It used to live in the per-user modules blob (MOD.discord), which could only
// ever describe one guild because the blob is keyed by broadcaster. With one
// broadcaster owning many guilds the blob keeps only the master switch and the
// Twitch login; everything per-guild moved here.
//
// The payload is the ddiscord.Config struct itself rather than a column per
// field. Config is the wire shape the dashboard already posts and the engine
// already parses, its json tags are camelCase, and it grows a field most weeks
// -- a column per field would make every one of those a migration for a value
// nothing queries on. Nothing filters or sorts by anything inside the blob;
// every read is by guild_id.
type GuildConfig struct {
	ent.Schema
}

// Fields of the GuildConfig.
func (GuildConfig) Fields() []ent.Field {
	return []ent.Field{
		field.String("guild_id").MaxLen(20).NotEmpty().Immutable(),

		// Denormalized from guild_bindings so a config row can be checked
		// against its caller without a join, and so an orphan row (binding
		// deleted, config not) is visible rather than silently ownerless.
		field.Uint64("broadcaster_id"),

		field.JSON("config", ddiscord.Config{}),

		// Optimistic concurrency. Two dashboard tabs open on the same guild
		// each hold a whole Config; a last-write-wins update would silently
		// drop whichever tab saved first. config.set carries the version the
		// caller read and is refused with `conflict` when it no longer
		// matches, which is the only way the losing tab can be told to reload.
		field.Int("version").Default(1),

		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

// Indexes of the GuildConfig.
func (GuildConfig) Indexes() []ent.Index {
	return []ent.Index{
		// One config per guild.
		index.Fields("guild_id").Unique(),
		// Not unique: a broadcaster owns many guilds.
		index.Fields("broadcaster_id"),
	}
}
