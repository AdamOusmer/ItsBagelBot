// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// MemberXP is one member's standing in one guild. The 60s per-message cooldown
// that decides whether a message earns XP at all stays in Valkey
// (discord:xpcd:<guild>:<user>): it is a rate limiter with a TTL, and moving it
// here would trade a local SET NX for a network round trip on every message.
// What lives here is the durable total, which the Valkey counter could lose to
// an eviction.
type MemberXP struct {
	ent.Schema
}

// Fields of the MemberXP.
func (MemberXP) Fields() []ent.Field {
	return []ent.Field{
		field.String("guild_id").MaxLen(20).NotEmpty().Immutable(),

		field.String("user_id").MaxLen(20).NotEmpty().Immutable(),

		field.Int64("xp").Default(0),

		// Denormalized from xp through discord.LevelOf on every write. Stored
		// rather than computed on read so the leaderboard and the rank card do
		// not each re-derive it, and so a change to the curve is a visible
		// migration instead of a silent re-scoring of everyone at once.
		field.Int("level").Default(0),

		// Nil for a member who has never claimed. The 24h window is measured
		// from this timestamp inside one transaction, which is what makes two
		// concurrent /daily calls award the bonus once.
		field.Time("last_daily").Optional().Nillable(),

		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

// Indexes of the MemberXP.
func (MemberXP) Indexes() []ent.Index {
	return []ent.Index{
		// The upsert conflict target and the /rank lookup.
		index.Fields("guild_id", "user_id").Unique(),
		// The leaderboard. The unique index above leads with guild_id,user_id
		// and so cannot serve an ordered-by-xp scan of one guild; without this
		// one every /top full-scans the table (same reasoning recorded on
		// loyalty's counters table).
		index.Fields("guild_id", "xp"),
	}
}
