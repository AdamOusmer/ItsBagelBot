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

const settingsKeyPrefix = "settings:"

const DefaultTTL = 24 * time.Hour

type Store struct {
	client  valkey.Client
	primary valkey.Client
}

func NewStore(client valkey.Client) *Store {
	return &Store{client: client, primary: pkg_valkey.Primary(client)}
}

type UserProjection struct {
	Status             string
	IsActive           bool
	Banned             bool
	Locale             string
	CommandsPageHidden bool
}

func (v *Store) SetUser(ctx context.Context, userID uint64, u UserProjection) error {
	return v.SetUserWithTTL(ctx, userID, u, DefaultTTL)
}

func (v *Store) SetUserWithTTL(ctx context.Context, userID uint64, u UserProjection, ttl time.Duration) error {

	defer segment(ctx, "HSET")()

	key := cache.UserKey(settingsKeyPrefix, userID)

	fields := v.client.B().Hset().
		Key(key).
		FieldValue().
		FieldValue("status", u.Status).
		FieldValue("active", utils.BoolField(u.IsActive)).
		FieldValue("banned", utils.BoolField(u.Banned)).
		FieldValue("commands_page_hidden", utils.BoolField(u.CommandsPageHidden))
	if u.Locale != "" {
		fields = fields.FieldValue("locale", u.Locale)
	}

	return v.pipelineWithTTL(ctx, key, ttl, fields.Build())
}

func (v *Store) GetUser(ctx context.Context, userID uint64) (status string, active, banned bool, locale string, commandsPageHidden bool, err error) {
	defer segment(ctx, "HGETALL")()

	key := cache.UserKey(settingsKeyPrefix, userID)

	res, err := v.client.Do(ctx, v.client.B().Hmget().Key(key).Field("status").Field("active").Field("banned").Field("locale").Field("commands_page_hidden").Build()).AsStrSlice()
	if err != nil {
		return "", false, false, "", false, err
	}

	if len(res) < 5 {
		return "", false, false, "", false, nil
	}

	return res[0], res[1] == "1", res[2] == "1", res[3], res[4] == "1", nil
}

type sectionWrite struct {
	prefixes []string
	marker   string
	ttl      time.Duration
	rows     [][2]string
}

func (v *Store) replaceSection(ctx context.Context, userID uint64, sec sectionWrite) error {
	key := cache.UserKey(settingsKeyPrefix, userID)
	if err := v.clearProjectionFields(ctx, key, sec.prefixes...); err != nil {
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

type sectionRead struct {
	prefix string
	marker string
}

func getSection[T any](ctx context.Context, v *Store, userID uint64, sec sectionRead) ([]T, bool, error) {
	defer segment(ctx, "HGETALL")()

	key := cache.UserKey(settingsKeyPrefix, userID)
	fields, err := v.client.Do(ctx, v.client.B().Hgetall().Key(key).Build()).AsStrMap()
	if err != nil {
		return nil, false, err
	}

	projected := fields[sec.marker] == "1"
	out := make([]T, 0)
	for field, value := range fields {
		name, ok := strings.CutPrefix(field, sec.prefix)
		if !ok || name == "" {
			continue
		}
		var row T
		if codec.Unmarshal([]byte(value), &row) != nil {
			continue
		}
		out = append(out, row)
	}
	return out, projected, nil
}

type HydrationState struct {
	User     bool
	Modules  bool
	Commands bool
}

func (s HydrationState) Complete() bool {
	return s.User && s.Modules && s.Commands
}

func (v *Store) GetHydrationState(ctx context.Context, userID uint64) (HydrationState, error) {
	defer segment(ctx, "HMGET")()

	key := cache.UserKey(settingsKeyPrefix, userID)
	fields, err := v.client.Do(ctx, v.client.B().Hmget().Key(key).
		Field("status").
		Field(modulesMarkerField).
		Field(commandsMarkerField).
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

	return v.client.Do(ctx, v.client.B().Hdel().Key(key).Field(stale...).Build()).Error()
}

func (v *Store) DeleteUser(ctx context.Context, userID uint64) error {

	defer segment(ctx, "DEL")()

	key := cache.UserKey(settingsKeyPrefix, userID)

	return v.client.Do(ctx, v.client.B().Del().Key(key).Build()).Error()
}

func (v *Store) Close() {
	v.client.Close()
}

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
