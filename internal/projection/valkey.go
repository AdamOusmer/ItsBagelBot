// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package projection

import (
	"context"
	"strings"
	"time"

	"ItsBagelBot/internal/utils"
	"ItsBagelBot/pkg/cache"
	"ItsBagelBot/pkg/codec"
	pkg_valkey "ItsBagelBot/pkg/valkey"

	"github.com/newrelic/go-agent/v3/newrelic"

	"github.com/valkey-io/valkey-go"
)

// The settings projection store: the settings:<user_id> hash, the user-level
// fields on it, and the plumbing every section shares (full-section
// replacement, the projected-marker and JSON field decoders, TTL and
// round-trip folding). The per-section accessors live beside it in
// valkey_commands.go, valkey_modules.go, valkey_stream.go and
// valkey_fetch.go: each section owns a disjoint set of hash fields, so they
// were split off rather than left clustered in one file.

const settingsKeyPrefix = "settings:"

// DefaultTTL is used by event-driven projection writes. Query-triggered
// hydration may request a shorter TTL, but projection expiry is monotonic: a
// shorter write never reduces a longer TTL already attached to the hash.
const DefaultTTL = 24 * time.Hour

// Store is the unified data access object for the settings projection. One hash per user:
//
//	settings:<user_id>
//	  status                  free | paid | vip
//	  active                  0 | 1
//	  live                    0 | 1
//	  module:<name>:enabled   0 | 1
//	  module:<name>:config    raw JSON
//
// Readers get everything they need for a chat message in a single HGETALL,
// without parsing anything but the module config they actually use. Every
// write is an overwrite, so replays and redeliveries are harmless.
type Store struct {
	client valkey.Client
	// primary serves the reads that must observe this Store's own writes.
	// Ordinary reads stay on client and its node-local route.
	primary valkey.Client
}

// NewStore creates a new Store instance using the provided Valkey client.
func NewStore(client valkey.Client) *Store {
	return &Store{client: client, primary: pkg_valkey.Primary(client)}
}

// UserProjection is the projected account state of one user: tier status, the
// receive/ban flags, and the UI locale.
type UserProjection struct {
	Status   string
	IsActive bool
	Banned   bool
	Locale   string
}

// SetUser projects the tier status, active flag, ban flag and UI locale of one
// user. An empty locale leaves the projected locale untouched (see
// SetUserWithTTL).
func (v *Store) SetUser(ctx context.Context, userID uint64, u UserProjection) error {
	return v.SetUserWithTTL(ctx, userID, u, DefaultTTL)
}

// SetUserWithTTL projects the user fields and keeps the hash for at least ttl.
// locale is written only when non-empty: cold-read write-backs (the status RPC)
// and older events that carry no locale must not overwrite a locale the full
// user projection already set.
func (v *Store) SetUserWithTTL(ctx context.Context, userID uint64, u UserProjection, ttl time.Duration) error {

	defer segment(ctx, "HSET")()

	key := cache.UserKey(settingsKeyPrefix, userID)

	fields := v.client.B().Hset().
		Key(key).
		FieldValue().
		FieldValue("status", u.Status).
		FieldValue("active", utils.BoolField(u.IsActive)).
		FieldValue("banned", utils.BoolField(u.Banned))
	if u.Locale != "" {
		fields = fields.FieldValue("locale", u.Locale)
	}

	return v.pipelineWithTTL(ctx, key, ttl, fields.Build())
}

// GetUser retrieves the tier status, active flag, ban flag and UI locale of one
// user. locale is empty when the hash predates locale projection.
func (v *Store) GetUser(ctx context.Context, userID uint64) (string, bool, bool, string, error) {
	defer segment(ctx, "HGETALL")()

	key := cache.UserKey(settingsKeyPrefix, userID)

	res, err := v.client.Do(ctx, v.client.B().Hmget().Key(key).Field("status").Field("active").Field("banned").Field("locale").Build()).AsStrSlice()
	if err != nil {
		return "", false, false, "", err
	}

	if len(res) < 4 {
		return "", false, false, "", nil
	}

	status := res[0]
	active := res[1] == "1"
	banned := res[2] == "1"
	locale := res[3]

	return status, active, banned, locale, nil
}

// sectionWrite is one full-section replacement: clear everything under
// prefix, then write the projected marker plus rows in one HSET, refreshing
// the TTL. The marker rides the SAME write as the rows on purpose — the
// projection-marker trust rule (markers may only ever come from full-section
// writes) is enforced by this being the only path that writes one.
type sectionWrite struct {
	prefix string
	marker string
	ttl    time.Duration
	rows   [][2]string
}

func (v *Store) replaceSection(ctx context.Context, userID uint64, sec sectionWrite) error {
	key := cache.UserKey(settingsKeyPrefix, userID)
	if err := v.clearProjectionFields(ctx, key, sec.prefix); err != nil {
		return err
	}
	fields := v.client.B().Hset().
		Key(key).
		FieldValue().
		FieldValue(sec.marker, "1")
	for _, row := range sec.rows {
		fields = fields.FieldValue(row[0], row[1])
	}
	return v.pipelineWithTTL(ctx, key, sec.ttl, fields.Build())
}

// markerProjected reads a section's :projected marker (nil = not projected).
func markerProjected(res valkey.ValkeyResult) (bool, error) {
	pj, err := res.ToString()
	if err != nil {
		if valkey.IsValkeyNil(err) {
			return false, nil
		}
		return false, err
	}
	return pj == "1", nil
}

// decodeJSONField decodes one JSON body straight off an HGET result. A nil
// field or an unparseable body is a clean miss (found=false, err=nil); only a
// real Valkey error propagates. Shared by the command and fetch row readers —
// the decode contract is identical by design (both views mirror their wire
// DTOs field-for-field).
func decodeJSONField[T any](res valkey.ValkeyResult) (T, bool, error) {
	var zero T
	body, err := res.ToString()
	if err != nil {
		if valkey.IsValkeyNil(err) {
			return zero, false, nil
		}
		return zero, false, err
	}
	var view T
	if codec.Unmarshal([]byte(body), &view) != nil {
		return zero, false, nil
	}
	return view, true, nil
}

// HydrationState describes which complete sections exist in a user's settings
// hash. The projected markers distinguish an intentionally empty collection
// from a cold cache miss.
type HydrationState struct {
	User     bool
	Modules  bool
	Commands bool
}

func (s HydrationState) Complete() bool {
	return s.User && s.Modules && s.Commands
}

// GetHydrationState checks every section with one HMGET. The live field is not
// part of configuration hydration; it is maintained independently by stream
// events.
func (v *Store) GetHydrationState(ctx context.Context, userID uint64) (HydrationState, error) {
	defer segment(ctx, "HMGET")()

	key := cache.UserKey(settingsKeyPrefix, userID)
	fields, err := v.client.Do(ctx, v.client.B().Hmget().Key(key).
		Field("status").
		Field("modules:projected").
		Field("commands:projected").
		Build()).AsStrSlice()
	if err != nil {
		return HydrationState{}, err
	}
	if len(fields) < 3 {
		return HydrationState{}, nil
	}
	return HydrationState{
		User:     fields[0] != "",
		Modules:  fields[1] == "1",
		Commands: fields[2] == "1",
	}, nil
}

func (v *Store) clearProjectionFields(ctx context.Context, key string, prefixes ...string) error {
	fields, err := v.client.Do(ctx, v.client.B().Hgetall().Key(key).Build()).AsStrMap()
	if err != nil {
		return err
	}

	stale := make([]string, 0, len(fields))
	for field := range fields {
		for _, prefix := range prefixes {
			if strings.HasPrefix(field, prefix) {
				stale = append(stale, field)
				break
			}
		}
	}
	if len(stale) == 0 {
		return nil
	}

	// One HDEL with every stale field instead of a round trip per field.
	return v.client.Do(ctx, v.client.B().Hdel().Key(key).Field(stale...).Build()).Error()
}

// DeleteUser drops the whole projection of one user.
func (v *Store) DeleteUser(ctx context.Context, userID uint64) error {

	defer segment(ctx, "DEL")()

	key := cache.UserKey(settingsKeyPrefix, userID)

	return v.client.Do(ctx, v.client.B().Del().Key(key).Build()).Error()
}

// Close releases the connection pool.
func (v *Store) Close() {
	v.client.Close()
}

// pipeline sends every command in a single round trip and returns the first
// command error. Folds an HSET and its EXPIRE (and, on a command delete, the
// HDEL) into one network round trip instead of two or three sequential Do calls.
func (v *Store) pipeline(ctx context.Context, cmds ...valkey.Completed) error {
	for _, res := range v.client.DoMulti(ctx, cmds...) {
		if err := res.Error(); err != nil {
			return err
		}
	}
	return nil
}

func (v *Store) pipelineWithTTL(ctx context.Context, key string, ttl time.Duration, cmds ...valkey.Completed) error {
	cmds = append(cmds, v.expiryCommands(key, ttl)...)
	return v.pipeline(ctx, cmds...)
}

// expiryCommands sets ttl on a persistent/new hash (NX), then extends an
// existing shorter expiry (GT). Together these commands implement max(current,
// requested) without a read/modify/write race, so a 2h query hydration can
// never shorten a 24h live-event projection.
func (v *Store) expiryCommands(key string, ttl time.Duration) []valkey.Completed {
	seconds := int64(ttl / time.Second)
	if seconds < 1 {
		seconds = 1
	}
	return []valkey.Completed{
		v.client.B().Expire().Key(key).Seconds(seconds).Nx().Build(),
		v.client.B().Expire().Key(key).Seconds(seconds).Gt().Build(),
	}
}

// segment reports the operation as a datastore segment of the transaction in
// ctx. New Relic has no Valkey product constant, so it reports under Redis,
// which is wire-compatible anyway. Without a transaction this is a no-op.
func segment(ctx context.Context, operation string) func() {

	txn := newrelic.FromContext(ctx)
	if txn == nil {
		return func() {}
	}

	seg := &newrelic.DatastoreSegment{
		StartTime:  txn.StartSegmentNow(),
		Product:    newrelic.DatastoreRedis,
		Collection: "settings",
		Operation:  operation,
	}

	return seg.End
}
