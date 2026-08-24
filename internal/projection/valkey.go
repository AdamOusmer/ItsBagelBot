// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package projection

import (
	"context"
	"strconv"
	"strings"
	"time"

	"ItsBagelBot/internal/domain/event/data"
	contract "ItsBagelBot/internal/domain/rpc/projection"
	"ItsBagelBot/internal/utils"
	"ItsBagelBot/pkg/cache"
	"ItsBagelBot/pkg/codec"
	pkg_valkey "ItsBagelBot/pkg/valkey"

	"github.com/newrelic/go-agent/v3/newrelic"

	"github.com/valkey-io/valkey-go"
)

const settingsKeyPrefix = "settings:"

// DefaultTTL is used by event-driven projection writes. Query-triggered
// hydration may request a shorter TTL, but projection expiry is monotonic: a
// shorter write never reduces a longer TTL already attached to the hash.
const DefaultTTL = 24 * time.Hour

// Command rows are addressable individually so a single lookup is one HGET, not
// a whole-hash HGETALL. commandFieldPrefix holds the command JSON keyed by its
// lower-cased primary name; aliasFieldPrefix holds alias -> primary-name
// pointers so an alias resolves in one extra HGET without scanning every row.
const (
	commandFieldPrefix = "command:"
	aliasFieldPrefix   = "cmdalias:"
	fetchFieldPrefix   = "fetch:"
)

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

// GetStreamLive reads the projected live/offline signal for one user. known is
// false when the field is absent (the projector has not seen a stream event and
// the hash has no live entry), letting the caller escalate instead of assuming
// offline.
func (v *Store) GetStreamLive(ctx context.Context, userID uint64) (live bool, known bool, err error) {
	defer segment(ctx, "HGET")()

	key := cache.UserKey(settingsKeyPrefix, userID)

	res, err := v.client.Do(ctx, v.client.B().Hget().Key(key).Field("live").Build()).ToString()
	if err != nil {
		if valkey.IsValkeyNil(err) {
			return false, false, nil
		}
		return false, false, err
	}
	return res == "1", true, nil
}

// SetStreamLive projects Twitch's current live/offline signal for one user.
func (v *Store) SetStreamLive(ctx context.Context, userID uint64, live bool) error {

	defer segment(ctx, "HSET")()

	key := cache.UserKey(settingsKeyPrefix, userID)

	return v.pipelineWithTTL(ctx, key, DefaultTTL,
		v.client.B().Hset().
			Key(key).
			FieldValue().
			FieldValue("live", utils.BoolField(live)).
			Build(),
	)
}

// SetModule projects one module row of one user. It deliberately does NOT set
// the modules:projected marker: a single-row event landing on a cold hash must
// not make a partial module list read as complete. Only the full-section
// writes (SetModules / SetModulesWithTTL) mark the section projected; until
// one runs, readers fall through to the projector RPC, whose miss path
// hydrates the full list.
func (v *Store) SetModule(ctx context.Context, userID uint64, mod ModuleView) error {

	defer segment(ctx, "HSET")()

	key := cache.UserKey(settingsKeyPrefix, userID)

	fields := v.client.B().Hset().
		Key(key).
		FieldValue().
		FieldValue("module:"+mod.Name+":enabled", utils.BoolField(mod.IsEnabled))

	if len(mod.Configs) > 0 {
		fields = fields.FieldValue("module:"+mod.Name+":config", string(mod.Configs))
	}

	return v.pipelineWithTTL(ctx, key, DefaultTTL, fields.Build())
}

type ModuleView = contract.ModuleView

type CommandView = contract.CommandView

func commandViewFromEvent(dto data.CommandChangedDTO) CommandView {
	allowed := ""
	if dto.AllowedUserID != 0 {
		allowed = strconv.FormatUint(dto.AllowedUserID, 10)
	}
	return CommandView{
		Name:             dto.Name,
		Aliases:          dto.Aliases,
		Response:         dto.Response,
		IsActive:         dto.IsActive,
		StreamOnlineOnly: dto.StreamOnlineOnly,
		Perm:             dto.Perm,
		Cooldown:         dto.Cooldown,
		AllowedUserID:    allowed,
		Uses:             dto.Uses,
	}
}

// SetModules projects a complete module list and records that an empty list is
// known data, not a cold Valkey miss.
func (v *Store) SetModules(ctx context.Context, userID uint64, modules []ModuleView) error {
	return v.SetModulesWithTTL(ctx, userID, modules, DefaultTTL)
}

// SetModulesWithTTL replaces the complete module section and keeps the hash
// for at least ttl. An empty list is still marked as projected.
func (v *Store) SetModulesWithTTL(ctx context.Context, userID uint64, modules []ModuleView, ttl time.Duration) error {
	defer segment(ctx, "HSET")()

	rows := make([][2]string, 0, 2*len(modules))
	for _, mod := range modules {
		rows = append(rows, [2]string{"module:" + mod.Name + ":enabled", utils.BoolField(mod.IsEnabled)})
		if len(mod.Configs) > 0 {
			rows = append(rows, [2]string{"module:" + mod.Name + ":config", string(mod.Configs)})
		}
	}
	return v.replaceSection(ctx, userID, sectionWrite{prefix: "module:", marker: "modules:projected", ttl: ttl, rows: rows})
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

// SetCommand projects one command row of one user. The command JSON lands under
// command:<name> and one alias:<alias> pointer is written per alias. The event
// carries only the new aliases, so the previous row is read first to retire any
// alias pointers that no longer apply (rename, reword, delete). That extra HGET
// is on the rare write path; the hot read path stays a single HGET.
func (v *Store) SetCommand(ctx context.Context, dto data.CommandChangedDTO) error {
	defer segment(ctx, "HSET")()

	key := cache.UserKey(settingsKeyPrefix, dto.UserID)
	name := strings.ToLower(dto.Name)
	field := commandFieldPrefix + name

	// The event carries only the new aliases, so retire the previous row's
	// alias pointers (rename, reword, delete) first. That extra HGET is on the
	// rare write path; the hot read path stays a single HGET.
	cmds := v.retireStaleAliases(ctx, key, field)

	if dto.Deleted {
		cmds = append(cmds, v.client.B().Hdel().Key(key).Field(field).Build())
		cmds = append(cmds, v.expiryCommands(key, DefaultTTL)...)
		return v.pipeline(ctx, cmds...)
	}

	set, err := v.commandSetCommand(key, field, name, dto)
	if err != nil {
		return err
	}
	cmds = append(cmds, set)
	cmds = append(cmds, v.expiryCommands(key, DefaultTTL)...)
	return v.pipeline(ctx, cmds...)
}

// retireStaleAliases reads the command's previous row and returns the HDEL (if
// any) that removes the alias pointers it no longer carries.
//
// This read is pinned to the primary: it is the read half of a
// read-modify-write over the row SetCommand is about to overwrite. A node-local
// replica that has not yet received the previous SetCommand returns an empty or
// older row, so the HDEL is either skipped or computed from the wrong alias
// list, and the retired aliases stay resolvable forever. Unlike a stale cache
// read this never converges, because nothing revisits the row. Only the reading
// side is pinned; the rest of the Store keeps node-local reads.
func (v *Store) retireStaleAliases(ctx context.Context, key, field string) []valkey.Completed {
	cmds := make([]valkey.Completed, 0, 4)
	old, _ := v.primary.Do(ctx, v.primary.B().Hget().Key(key).Field(field).Build()).ToString()
	if old == "" {
		return cmds
	}
	var prev CommandView
	if codec.Unmarshal([]byte(old), &prev) != nil || len(prev.Aliases) == 0 {
		return cmds
	}
	stale := make([]string, 0, len(prev.Aliases))
	for _, a := range prev.Aliases {
		stale = append(stale, aliasFieldPrefix+strings.ToLower(a))
	}
	return append(cmds, v.client.B().Hdel().Key(key).Field(stale...).Build())
}

// commandSetCommand builds the HSET writing the command body plus its alias
// pointers. Like SetModule, it never sets commands:projected: a single event
// row on a cold hash must not make the command section read as complete —
// only SetCommands / SetCommandsWithTTL (full-list writes) set the marker.
func (v *Store) commandSetCommand(key, field, name string, dto data.CommandChangedDTO) (valkey.Completed, error) {
	view := commandViewFromEvent(dto)
	body, err := codec.Marshal(view)
	if err != nil {
		return valkey.Completed{}, err
	}
	set := v.client.B().Hset().Key(key).FieldValue().
		FieldValue(field, string(body))
	for _, a := range view.Aliases {
		set = set.FieldValue(aliasFieldPrefix+strings.ToLower(a), name)
	}
	return set.Build(), nil
}

// GetCommand reads one command by the name (or alias) a viewer typed, in a
// single round trip. found reports whether the command exists; projected
// reports whether the command section has been populated at all, so a caller
// can tell a real "no such command" from a cold Valkey miss that should fall
// through to the projector RPC.
func (v *Store) GetCommand(ctx context.Context, userID uint64, name string) (view CommandView, found bool, projected bool, err error) {
	defer segment(ctx, "HGET")()

	key := cache.UserKey(settingsKeyPrefix, userID)
	lname := strings.ToLower(name)

	res := v.client.DoMulti(ctx,
		v.client.B().Hget().Key(key).Field(commandFieldPrefix+lname).Build(),
		v.client.B().Hget().Key(key).Field(aliasFieldPrefix+lname).Build(),
		v.client.B().Hget().Key(key).Field("commands:projected").Build(),
	)

	projected, err = commandsProjected(res[2])
	if err != nil {
		return CommandView{}, false, false, err
	}

	// Direct hit: the typed name is a command's own field.
	view, found, err = decodeCommandField(res[0])
	if err != nil {
		return CommandView{}, false, projected, err
	}
	if found {
		return view, true, true, nil
	}

	// Alias hit: the typed name points at another command's field, read next.
	return v.resolveAlias(ctx, key, res[1], projected)
}

// commandsProjected reads the commands:projected marker (nil = not projected).
func commandsProjected(res valkey.ValkeyResult) (bool, error) {
	pj, err := res.ToString()
	if err != nil {
		if valkey.IsValkeyNil(err) {
			return false, nil
		}
		return false, err
	}
	return pj == "1", nil
}

// decodeCommandField decodes a command body straight off an HGET result. A nil
// field or an unparseable body is a clean miss (found=false, err=nil); only a
// real Valkey error propagates.
func decodeCommandField(res valkey.ValkeyResult) (CommandView, bool, error) {
	body, err := res.ToString()
	if err != nil {
		if valkey.IsValkeyNil(err) {
			return CommandView{}, false, nil
		}
		return CommandView{}, false, err
	}
	var view CommandView
	if codec.Unmarshal([]byte(body), &view) != nil {
		return CommandView{}, false, nil
	}
	return view, true, nil
}

// resolveAlias follows an alias pointer to the command it names and reads that
// command's body in one more round trip. A missing or dangling alias is a clean
// miss.
func (v *Store) resolveAlias(ctx context.Context, key string, aliasRes valkey.ValkeyResult, projected bool) (CommandView, bool, bool, error) {
	primary, err := aliasRes.ToString()
	if err != nil {
		if valkey.IsValkeyNil(err) {
			return CommandView{}, false, projected, nil
		}
		return CommandView{}, false, projected, err
	}
	if primary == "" {
		return CommandView{}, false, projected, nil
	}

	view, found, err := decodeCommandField(v.client.Do(ctx, v.client.B().Hget().Key(key).Field(commandFieldPrefix+primary).Build()))
	if err != nil || !found {
		return CommandView{}, false, projected, err
	}
	return view, true, true, nil
}

// SetCommands projects a complete command list and records that an empty list is
// known data, not a cold Valkey miss.
func (v *Store) SetCommands(ctx context.Context, userID uint64, commands []CommandView) error {
	return v.SetCommandsWithTTL(ctx, userID, commands, DefaultTTL)
}

// SetCommandsWithTTL replaces the complete command section and keeps the hash
// for at least ttl. An empty list is still marked as projected.
func (v *Store) SetCommandsWithTTL(ctx context.Context, userID uint64, commands []CommandView, ttl time.Duration) error {
	defer segment(ctx, "HSET")()

	key := cache.UserKey(settingsKeyPrefix, userID)
	if err := v.clearProjectionFields(ctx, key, commandFieldPrefix, aliasFieldPrefix); err != nil {
		return err
	}

	fields := v.client.B().Hset().
		Key(key).
		FieldValue().
		FieldValue("commands:projected", "1")

	for _, cmd := range commands {
		body, err := codec.Marshal(cmd)
		if err != nil {
			return err
		}
		name := strings.ToLower(cmd.Name)
		fields = fields.FieldValue(commandFieldPrefix+name, string(body))
		for _, a := range cmd.Aliases {
			fields = fields.FieldValue(aliasFieldPrefix+strings.ToLower(a), name)
		}
	}

	return v.pipelineWithTTL(ctx, key, ttl, fields.Build())
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

func (v *Store) GetModules(ctx context.Context, userID uint64) ([]ModuleView, bool, error) {
	defer segment(ctx, "HGETALL")()

	key := cache.UserKey(settingsKeyPrefix, userID)
	fields, err := v.client.Do(ctx, v.client.B().Hgetall().Key(key).Build()).AsStrMap()
	if err != nil {
		return nil, false, err
	}

	// projected trusts the marker alone: module rows written by single-module
	// events (SetModule) never set it, so a partial hash correctly reads as
	// not-yet-projected and the caller falls through to the full hydration.
	projected := fields["modules:projected"] == "1"
	byName := map[string]ModuleView{}
	for field, value := range fields {
		name, suffix, ok := parseModuleField(field)
		if !ok {
			continue
		}
		mod := byName[name]
		mod.Name = name
		switch suffix {
		case "enabled":
			mod.IsEnabled = value == "1"
		case "config":
			mod.Configs = codec.RawMessage(value)
		}
		byName[name] = mod
	}

	out := make([]ModuleView, 0, len(byName))
	for _, mod := range byName {
		out = append(out, mod)
	}
	return out, projected, nil
}

func (v *Store) GetCommands(ctx context.Context, userID uint64) ([]CommandView, bool, error) {
	defer segment(ctx, "HGETALL")()

	key := cache.UserKey(settingsKeyPrefix, userID)
	fields, err := v.client.Do(ctx, v.client.B().Hgetall().Key(key).Build()).AsStrMap()
	if err != nil {
		return nil, false, err
	}

	// projected trusts the marker alone (see GetModules): per-command event
	// rows never set it, so a partial hash falls through to full hydration.
	projected := fields["commands:projected"] == "1"
	out := make([]CommandView, 0)
	for field, value := range fields {
		name, ok := strings.CutPrefix(field, "command:")
		if !ok || name == "" {
			continue
		}
		var cmd CommandView
		if err := codec.Unmarshal([]byte(value), &cmd); err != nil {
			continue
		}
		out = append(out, cmd)
	}
	return out, projected, nil
}

// SetFetch projects one $(urlfetch) definition of one user, the fetch twin of
// SetCommand minus alias pointers: definitions have no aliases. A Deleted
// event (rename or delete; rows hard-delete) retires the fetch:<name> field
// via the same HDEL path. Like every per-row setter it deliberately does NOT
// set the fetch:projected marker — only the full-section write may declare
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
	return v.replaceSection(ctx, userID, sectionWrite{prefix: fetchFieldPrefix, marker: "fetch:projected", ttl: ttl, rows: rows})
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
		v.client.B().Hget().Key(key).Field("fetch:projected").Build(),
	)

	projected, err = fetchSectionProjected(res[1])
	if err != nil {
		return FetchView{}, false, false, err
	}

	view, found, err = decodeFetchField(res[0])
	if err != nil {
		return FetchView{}, false, projected, err
	}
	return view, found, projected, nil
}

// fetchSectionProjected reads the fetch:projected marker (nil = not
// projected).
func fetchSectionProjected(res valkey.ValkeyResult) (bool, error) {
	pj, err := res.ToString()
	if err != nil {
		if valkey.IsValkeyNil(err) {
			return false, nil
		}
		return false, err
	}
	return pj == "1", nil
}

// decodeFetchField decodes a definition body straight off an HGET result. A
// nil field or an unparseable body is a clean miss (found=false, err=nil);
// only a real Valkey error propagates.
func decodeFetchField(res valkey.ValkeyResult) (FetchView, bool, error) {
	body, err := res.ToString()
	if err != nil {
		if valkey.IsValkeyNil(err) {
			return FetchView{}, false, nil
		}
		return FetchView{}, false, err
	}
	var view FetchView
	if codec.Unmarshal([]byte(body), &view) != nil {
		return FetchView{}, false, nil
	}
	return view, true, nil
}

// GetFetches reads the complete projected definition list of one user,
// mirroring GetCommands: the fetch:projected marker alone decides whether a
// missing row means "none" or "not yet hydrated".
func (v *Store) GetFetches(ctx context.Context, userID uint64) ([]FetchView, bool, error) {
	defer segment(ctx, "HGETALL")()

	key := cache.UserKey(settingsKeyPrefix, userID)
	fields, err := v.client.Do(ctx, v.client.B().Hgetall().Key(key).Build()).AsStrMap()
	if err != nil {
		return nil, false, err
	}

	projected := fields["fetch:projected"] == "1"
	out := make([]FetchView, 0)
	for field, value := range fields {
		name, ok := strings.CutPrefix(field, fetchFieldPrefix)
		if !ok || name == "" || name == "projected" {
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

func parseModuleField(field string) (name, suffix string, ok bool) {
	rest, found := strings.CutPrefix(field, "module:")
	if !found {
		return "", "", false
	}
	idx := strings.LastIndex(rest, ":")
	if idx < 0 {
		return "", "", false
	}
	return rest[:idx], rest[idx+1:], true
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
