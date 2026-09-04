// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// User holds the schema definition for the User entity.
type User struct {
	ent.Schema
}

// Fields of the User.
func (User) Fields() []ent.Field {

	return []ent.Field{

		field.Uint64("id").Unique().Immutable(), // This is the primary key -- Getting it from Twitch User ID

		field.String("username").NotEmpty(),

		field.String("email").NotEmpty().Unique().Sensitive(),

		// Real contact email captured at Twitch login (user:read:email scope),
		// stored as a Tink AEAD envelope bound to the user id (AAD) so a
		// database leak never exposes addresses. Absent until the user's next
		// login. Deleted with the row, so account erasure covers it. The
		// legacy "email" column above stays a synthetic placeholder.
		field.Bytes("email_enc").Optional().Sensitive(),

		field.Bool("is_active").Default(true),

		field.Bool("banned").Default(false),

		field.Enum("status").
			Values("free", "paid", "vip"). // vip is a permanent paid tier
			Default("free"),

		// UI language preference for the console. Chosen at onboarding, editable
		// in settings, and mirrored into a cookie so the SSR render is fast. A
		// plain string (not an enum) so shipping a new locale needs no schema
		// migration; the console validates the value against its locale set.
		field.String("locale").Default("en").MaxLen(8),

		// Console UI preference: whether the animated custom cursor is shown. Off
		// falls back to the native pointer. Console-only (the worker never reads
		// it); mirrored into a cookie for fast SSR. Existing rows migrate to true,
		// preserving the current behaviour.
		field.Bool("custom_cursor").Default(true),

		field.String("creator_code").Optional().Nillable().MaxLen(64),

		// Billing ownership is deliberately stored with the user tier. This lets
		// webhook retries apply idempotently and prevents a Tebex cancellation
		// from revoking a staff grant or permanent VIP status.
		field.String("subscription_source").Default(""),
		field.Time("subscription_expires_at").Optional().Nillable(),
		field.String("subscription_ref").Optional().Nillable(),
		field.Bool("subscription_cancel_pending").Default(false),
		field.Time("billing_event_at").Optional().Nillable(),
		field.String("billing_event_id").Optional().Nillable(),

		// Number of premium gifts this user has paid for (as the gifter). Bumped
		// once per gift when the payment lands, on the idempotent billing-apply
		// Existing rows get 0 on migration.
		field.Uint32("gifts_sent").Default(0),

		field.Bool("onboarded").Default(false),

		field.Time("created_at").Default(time.Now),

		field.Time("updated_at").
			Default(time.Now).
			UpdateDefault(time.Now),
	}

}

// Edges of the User.
func (User) Edges() []ent.Edge {

	return []ent.Edge{

		edge.To("tokens", Tokens.Type).
			Annotations(entsql.Annotation{
				OnDelete: entsql.Cascade,
			}),
	}
}

func (User) Indexes() []ent.Index {
	return []ent.Index{
		// Covers Users.ExpireSubscriptions' sweep predicate (app/users/repository/billing.go),
		// which was measured live at 87,561 executions / 1,632,597 rows examined /
		// 0 rows sent over 20.6 days uptime (100% SUM_NO_INDEX_USED, 58% of all
		// application server-side DB time) -- EXPLAIN showed type=ALL, key=NULL,
		// a full table scan every 20s (ticker fires every minute x3 replicas,
		// no leader election). The query is:
		//   status = 'paid' AND subscription_expires_at IS NOT NULL AND (
		//     (subscription_source = 'admin' AND subscription_expires_at <= now) OR
		//     (subscription_source = 'tebex' AND subscription_expires_at <= now-grace)
		//   )
		// Column order is deliberate: status is the equality predicate that
		// eliminates the overwhelming majority of rows ('free'/'vip' never match,
		// and today only a handful of users are 'paid' at all), so it belongs
		// leftmost. subscription_source is the second equality predicate -- each
		// OR branch pins it to exactly one literal ('admin' or 'tebex') -- so
		// putting it before the range column keeps both branches served by the
		// same index prefix (MySQL executes this OR-of-equal-conjunctions as two
		// range scans over the same (status, subscription_source) prefix and
		// merges them). subscription_expires_at is last because it's the only
		// range predicate; range columns must trail every equality column in a
		// composite index or the remaining columns can't be used for seeking.
		// This also makes the index covering for the .Select(id, subscription_source)
		// narrowing below: InnoDB secondary indexes always carry the PK (id), and
		// subscription_source is already a key column, so the read never touches
		// the clustered row (no email_enc / billing-block hydration) -- was
		// previously type=ALL scanning all 19 rows (and every row thereafter) for
		// zero matches; at 10k users that full scan becomes ~43M rows/day.
		index.Fields("status", "subscription_source", "subscription_expires_at"),

		// Backs the login -> id resolve behind the public command page
		// (/user/<login>), which is unauthenticated and cacheable by anyone
		// with the URL. Without it that lookup is a full table scan on every
		// cold cache entry, from an endpoint no login gates -- the same shape
		// the sweep predicate above cost us before it was indexed.
		// Deliberately non-unique: a Twitch rename frees the old login for
		// someone else, so two rows can legitimately carry the same username
		// until the renamed user next signs in and the upsert refreshes it.
		// The resolve takes the most recently updated row (see
		// Users.IDByUsername) rather than rejecting the collision.
		index.Fields("username"),
	}
}
