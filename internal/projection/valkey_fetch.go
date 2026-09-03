// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package projection

// $(urlfetch) definition projection: the fetch:<name> rows, their projected
// marker and the readers gossip resolves definitions through. Split from
// valkey.go so the fetch section reads as one file beside the command and
// module sections it mirrors.

import (
	"context"
	"strings"
	"time"

	"ItsBagelBot/internal/domain/event/data"
	"ItsBagelBot/pkg/cache"
	"ItsBagelBot/pkg/codec"

	"github.com/valkey-io/valkey-go"
)

// fetchFieldPrefix holds the fetch definition JSON keyed by its lower-cased
// name, addressable individually so resolving one definition is a single
// HGET rather than a whole-hash HGETALL (the commandFieldPrefix precedent).
const fetchFieldPrefix = "fetch:"

// fetchesMarkerField is the fetch section's completeness marker, deliberately
// PLURAL so it does not share fetchFieldPrefix. $(urlfetch) names are
// user-supplied, so with the marker at fetch:projected a user naming a
// definition "projected" made SetFetch write that definition's JSON over the
// marker; markerProjected accepts only "1", so the section then read as never
// projected and every GetFetch fell through to the projector RPC for good.
// commands:projected and modules:projected already keep a plural marker prefix
// distinct from their singular command:/module: row prefixes — this follows
// that convention rather than inventing one. Rejected: rejecting "projected"
// as a definition name, which pushes a Valkey field-layout detail into
// user-facing validation and would still break every name a future marker
// takes.
const fetchesMarkerField = "fetches:projected"

// FetchView is the projected view of one $(urlfetch) definition, stored as
// the JSON body of the fetch:<name> hash field. Field set and json tags match
// internal/domain/rpc/fetchkey.FetchView exactly (the CommandView/contract
// duplication precedent) so gossip and the console decode without conversion.
// It carries key_label only: sealed key material never enters Valkey or any
// cache — plaintext travels the single key RPC per fetch.
type FetchView struct {
	Name     string   `json:"name"`
	URL      string   `json:"url"`
	JSONPath []string `json:"json_path,omitempty"`
	KeyLabel string   `json:"key_label,omitempty"`
	IsActive bool     `json:"is_active"`
}

// SetFetch projects one $(urlfetch) definition of one user, the fetch twin of
// SetCommand minus alias pointers: definitions have no aliases. A Deleted
// event (rename or delete; rows hard-delete) retires the fetch:<name> field
// via the same HDEL path. Like every per-row setter it deliberately does NOT
// set the fetches:projected marker — only the full-section write may declare
// completeness.
func (v *Store) SetFetch(ctx context.Context, dto data.FetchChangedDTO) error {
	defer segment(ctx, "HSET")()

	key := cache.UserKey(settingsKeyPrefix, dto.UserID)
	field := fetchFieldPrefix + strings.ToLower(dto.Name)

	if dto.Deleted {
		cmds := append(
			[]valkey.Completed{v.client.B().Hdel().Key(key).Field(field).Build()},
			v.expiryCommands(key, DefaultTTL)...,
		)
		return v.pipeline(ctx, cmds...)
	}

	body, err := codec.Marshal(FetchView{
		Name:     strings.ToLower(dto.Name),
		URL:      dto.URL,
		JSONPath: dto.JSONPath,
		KeyLabel: dto.KeyLabel,
		IsActive: dto.IsActive,
	})
	if err != nil {
		return err
	}

	cmds := append(
		[]valkey.Completed{
			v.client.B().Hset().Key(key).FieldValue().FieldValue(field, string(body)).Build(),
		},
		v.expiryCommands(key, DefaultTTL)...,
	)
	return v.pipeline(ctx, cmds...)
}

// SetFetches projects a complete definition list and records that an empty
// list is known data, not a cold Valkey miss.
func (v *Store) SetFetches(ctx context.Context, userID uint64, fetches []FetchView) error {
	return v.SetFetchesWithTTL(ctx, userID, fetches, DefaultTTL)
}

// SetFetchesWithTTL replaces the complete fetch section and keeps the hash
// for at least ttl. An empty list is still marked as projected.
func (v *Store) SetFetchesWithTTL(ctx context.Context, userID uint64, fetches []FetchView, ttl time.Duration) error {
	defer segment(ctx, "HSET")()

	rows := make([][2]string, 0, len(fetches))
	for _, f := range fetches {
		body, err := codec.Marshal(f)
		if err != nil {
			return err
		}
		rows = append(rows, [2]string{fetchFieldPrefix + strings.ToLower(f.Name), string(body)})
	}
	return v.replaceSection(ctx, userID, sectionWrite{prefix: fetchFieldPrefix, marker: fetchesMarkerField, ttl: ttl, rows: rows})
}

// GetFetch reads one definition by name in a single round trip. found reports
// whether the definition exists; projected reports whether the fetch section
// has been populated at all, so a caller can tell a real "no such definition"
// from a cold Valkey miss that should fall through to the projector RPC.
func (v *Store) GetFetch(ctx context.Context, userID uint64, name string) (view FetchView, found bool, projected bool, err error) {
	defer segment(ctx, "HGET")()

	key := cache.UserKey(settingsKeyPrefix, userID)
	lname := strings.ToLower(name)

	res := v.client.DoMulti(ctx,
		v.client.B().Hget().Key(key).Field(fetchFieldPrefix+lname).Build(),
		v.client.B().Hget().Key(key).Field(fetchesMarkerField).Build(),
	)

	projected, err = markerProjected(res[1])
	if err != nil {
		return FetchView{}, false, false, err
	}

	view, found, err = decodeJSONField[FetchView](res[0])
	if err != nil {
		return FetchView{}, false, projected, err
	}
	return view, found, projected, nil
}

// fetchRowName extracts the definition name from a hash field: "" for
// anything that is not a fetch row. Every fetch:<name> field is now a row:
// the section marker moved out of this prefix (see fetchesMarkerField), so the
// guard that used to drop name == "projected" is gone with it. Keeping it
// would have made a definition a user legitimately named "projected"
// permanently invisible in GetFetches — the same bug moved one field over.
// Hashes written before the rename still carry fetch:projected = "1"; "1" is
// not a FetchView, so GetFetches' unmarshal skips it like any other corrupt
// row, and the next full-section write clears it with the rest of the prefix.
func fetchRowName(field string) string {
	name, ok := strings.CutPrefix(field, fetchFieldPrefix)
	if !ok {
		return ""
	}
	return name
}

// GetFetches reads the complete projected definition list of one user,
// mirroring GetCommands: the fetches:projected marker alone decides whether a
// missing row means "none" or "not yet hydrated".
func (v *Store) GetFetches(ctx context.Context, userID uint64) ([]FetchView, bool, error) {
	defer segment(ctx, "HGETALL")()

	key := cache.UserKey(settingsKeyPrefix, userID)
	fields, err := v.client.Do(ctx, v.client.B().Hgetall().Key(key).Build()).AsStrMap()
	if err != nil {
		return nil, false, err
	}

	projected := fields[fetchesMarkerField] == "1"
	out := make([]FetchView, 0)
	for field, value := range fields {
		if fetchRowName(field) == "" {
			continue
		}
		var f FetchView
		if err := codec.Unmarshal([]byte(value), &f); err != nil {
			continue
		}
		out = append(out, f)
	}
	return out, projected, nil
}
