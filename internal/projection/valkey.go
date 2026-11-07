// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package projection

import (
	"context"
	"strconv"
	"strings"
	"time"

	"ItsBagelBot/internal/domain/lifecycle"
	"ItsBagelBot/internal/utils"
	"ItsBagelBot/internal/watchtime"
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
	Lifecycle        *lifecycle.State
	StateRevision    int64
	AccountCreatedAt int64
	Status           string
	IsActive         bool
	Banned           bool
	Locale           string
	// CommandsPageHidden mirrors the inverted flag (D2): written unconditionally
	// (unlike Locale below), so an absent hash field decodes as false, meaning
	// visible -- the pre-feature behaviour needs no "skip when empty" rule.
	CommandsPageHidden    bool
	LeaderboardPageHidden bool
	CreatorCode           string
}

func (v *Store) SetUser(ctx context.Context, userID uint64, u UserProjection) error {
	return v.SetUserWithTTL(ctx, userID, u, DefaultTTL)
}

func (v *Store) SetUserWithTTL(ctx context.Context, userID uint64, u UserProjection, ttl time.Duration) error {

	defer segment(ctx, "HSET")()
	// Hydration and status write-back share this path with ordinary events.
	// Restore the canonical incarnation before applying revision fencing; the
	// final script still rejects an intervening deletion or recreation.
	if u.AccountCreatedAt > 0 {
		restored, err := v.RestoreAccount(ctx, userID, u.AccountCreatedAt)
		if err != nil || !restored {
			return err
		}
	}

	key := cache.UserKey(settingsKeyPrefix, userID)

	runtime, err := codec.Marshal(u.Lifecycle)
	if err != nil {
		return err
	}
	args := []string{utils.BoolField(u.IsActive), utils.BoolField(u.Banned), u.Status, utils.BoolField(u.CommandsPageHidden), u.Locale, strconv.FormatInt(max(1, int64(ttl/time.Second)), 10), strconv.FormatInt(u.AccountCreatedAt, 10), strconv.FormatInt(u.StateRevision, 10), string(runtime)}
	background := u.IsActive && !u.Banned
	if u.Lifecycle != nil {
		background = background && u.Lifecycle.AllowsBackground()
	}
	args = append(args, utils.BoolField(background), utils.BoolField(u.LeaderboardPageHidden), u.CreatorCode)
	return pkg_valkey.RunHashRevisionGateUser(ctx, v.primary, []string{key, watchtime.AdmissionKey(userID)}, args).Error()
}

// GetUser retrieves the tier status, active flag, ban flag, UI locale and
// commands-page-hidden flag of one user. locale is empty when the hash
// predates locale projection; commandsPageHidden reads false the same way
// when the hash predates this field (D2's absent-means-visible rule).
func (v *Store) GetUser(ctx context.Context, userID uint64) (status string, active, banned bool, locale string, commandsPageHidden bool, err error) {
	defer segment(ctx, "HMGET")()

	key := cache.UserKey(settingsKeyPrefix, userID)

	res, err := pkg_valkey.Exec1(ctx, v.client, pkg_valkey.HMGetCmd(v.client, key, "status", "active", "banned", "locale", "commands_page_hidden")).AsStrSlice()
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
	pairs := make([]pkg_valkey.HashFieldValue, 0, len(sec.rows)+1)
	pairs = append(pairs, pkg_valkey.HashFieldValue{Field: sec.marker, Value: "1"})
	for _, row := range sec.rows {
		pairs = append(pairs, pkg_valkey.HashFieldValue{Field: row[0], Value: row[1]})
	}
	return v.pipelineWithTTL(ctx, key, sec.ttl, pkg_valkey.HSetCmd(v.client, key, pairs...))
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
	fields, err := pkg_valkey.HashAll(ctx, v.client, key)
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
	fields, err := pkg_valkey.Exec1(ctx, v.client, pkg_valkey.HMGetCmd(v.client, key, "status", modulesMarkerField, commandsMarkerField)).AsStrSlice()
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
	fields, err := pkg_valkey.HashAll(ctx, v.client, key)
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

	return pkg_valkey.Exec1(ctx, v.client, pkg_valkey.HDelCmd(v.client, key, stale...)).Error()
}

func (v *Store) DeleteUser(ctx context.Context, userID uint64) error {
	_, err := v.DeleteLegacyAccount(ctx, userID)
	return err
}

// DeleteLegacyAccount refuses an unstamped delete once a current incarnation
// is known. Its result also fences callers' subsequent cleanup side effects.
func (v *Store) DeleteLegacyAccount(ctx context.Context, userID uint64) (bool, error) {
	defer segment(ctx, "DEL")()
	key := cache.UserKey(settingsKeyPrefix, userID)
	n, err := pkg_valkey.RunHashRevisionGateLegacyDelete(ctx, v.primary, []string{key, watchtime.AdmissionKey(userID)}, nil).AsInt64()
	return n == 1, err
}

func (v *Store) Close() {
	v.client.Close()
}

func (v *Store) pipeline(ctx context.Context, cmds ...valkey.Completed) error {
	for _, res := range pkg_valkey.ExecMulti(ctx, v.client, cmds...) {
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
		pkg_valkey.ExpireNXCmd(v.client, key, seconds),
		pkg_valkey.ExpireGTCmd(v.client, key, seconds),
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
