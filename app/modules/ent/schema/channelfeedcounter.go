// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// ChannelFeedCounter is the per-channel half of the "feed the bagel" tally:
// one row per broadcaster, count only ever up. FeedCounter stays the fleet-wide
// lifetime total (one bagel, every channel feeds it); this table answers the
// other question chat asks, which channel feeds it most, and is the source of
// truth behind the feed leaderboard. Sesame's valkey holds a live sorted-set
// view of these rows for the hot path; these rows survive it.
type ChannelFeedCounter struct {
	ent.Schema
}

// Fields of the ChannelFeedCounter.
func (ChannelFeedCounter) Fields() []ent.Field {
	return []ent.Field{
		// The broadcaster's Twitch id doubles as the primary key: one row per
		// channel, no surrogate id to look up on the bump path.
		field.Uint64("id").Unique().Immutable(),
		field.Uint64("count").Default(0),

		// Display name at the last feeding, so a leaderboard line can name the
		// channel without a users-service round trip per entry. Mutable: a
		// broadcaster who renames is shown under the new name from then on.
		field.String("name").Default("").MaxLen(64),
	}
}

// Indexes of the ChannelFeedCounter.
func (ChannelFeedCounter) Indexes() []ent.Index {
	return []ent.Index{
		// Both leaderboard reads (top N, and "how many channels beat this
		// count") order or filter on count alone.
		index.Fields("count"),
	}
}
