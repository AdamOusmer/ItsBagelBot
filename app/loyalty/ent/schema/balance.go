package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// Balance is one viewer's loyalty standing in one channel: spendable points
// and accumulated watch time. The broadcaster and the viewer are referenced by
// Twitch id only (the users service owns broadcaster accounts; viewers are
// plain chatters with no account at all). One row per (broadcaster, viewer),
// created lazily on the viewer's first accrual and only ever grown by additive
// batch flushes — the service stores standings, never events, so the table
// grows with distinct viewers, not with activity.
type Balance struct {
	ent.Schema
}

// Fields of the Balance.
func (Balance) Fields() []ent.Field {
	return []ent.Field{
		field.Uint64("user_id").Immutable(),

		field.Uint64("viewer_id").Immutable(),

		// Display identity, refreshed opportunistically from whichever event
		// last carried it. Twitch logins are ≤25 chars; display names ≤25 too
		// but may be multi-byte.
		field.String("viewer_login").Optional().MaxLen(64),
		field.String("viewer_name").Optional().MaxLen(64),

		// Spendable points. Signed so a future spend path can never underflow
		// the column type; accruals keep it non-negative today.
		field.Int64("points").Default(0),

		// Lifetime watch time in seconds, summed from the watch ticks.
		field.Uint64("watch_seconds").Default(0),

		field.Time("created_at").Default(time.Now),

		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

func (Balance) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("user_id", "viewer_id").
			Unique(),
		// Leaderboard reads: top-N by points within one channel.
		index.Fields("user_id", "points"),
	}
}
