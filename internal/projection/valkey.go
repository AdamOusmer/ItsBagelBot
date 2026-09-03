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

// Per-stream metadata and counter-baseline fields live in the same
// settings:<user_id> hash as everything else above (LIVE FIELD PRECEDENT:
// see SetStreamLive/GetStreamLive). streamTitleField doubles as the presence
// anchor for GetStreamInfo's known check; streamCtrMessagesField is the same
// anchor for GetStreamCounterBaseline.
const (
	streamTitleField       = "stream:title"
	streamGameField        = "stream:game"
	streamViewersField     = "stream:viewers"
	streamPeakViewersField = "stream:peak_viewers"
	streamStartedAtField   = "stream:started_at"
	streamEndedAtField     = "stream:ended_at"

	streamCtrMessagesField  = "streamctr:messages"
	streamCtrAnsweredField  = "streamctr:answered"
	streamCtrModActionField = "streamctr:mod_actions"
)

// streamUserKey parses the RPC-carried string broadcaster id (the shape
// StreamInfoRequest/StreamCounters callers already hold, straight off the
// wire) into the uint64 cache.UserKey expects. The stream-info and
// counter-baseline accessors take a string id instead of matching the rest
// of Store's uint64 convention, so this is the one place that parse happens.
func streamUserKey(userID string) (string, error) {
	id, err := strconv.ParseUint(userID, 10, 64)
	if err != nil {
		return "", err
	}
	return cache.UserKey(settingsKeyPrefix, id), nil
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

// StreamInfo is the projected per-stream metadata shown on the Overview
// dashboard: title/game as Twitch reports them, the current and peak viewer
// counts, and the stream's start/end timestamps. It rides the same
// settings:<user_id> hash as every other LIVE FIELD (no :projected marker:
// see the sectionWrite comment above and clearProjectionFields below for why
// markers are reserved for full-section collection writes).
type StreamInfo struct {
	Title       string
	GameName    string
	ViewerCount int
	PeakViewers int
	StartedAt   time.Time
	EndedAt     time.Time
}

// SetStreamInfo projects Twitch's current stream metadata for one user.
func (v *Store) SetStreamInfo(ctx context.Context, userID string, info StreamInfo) error {
	defer segment(ctx, "HSET")()

	key, err := streamUserKey(userID)
	if err != nil {
		return err
	}

	return v.pipelineWithTTL(ctx, key, DefaultTTL,
		v.client.B().Hset().
			Key(key).
			FieldValue().
			FieldValue(streamTitleField, info.Title).
			FieldValue(streamGameField, info.GameName).
			FieldValue(streamViewersField, strconv.Itoa(info.ViewerCount)).
			FieldValue(streamPeakViewersField, strconv.Itoa(info.PeakViewers)).
			FieldValue(streamStartedAtField, info.StartedAt.Format(time.RFC3339)).
			FieldValue(streamEndedAtField, info.EndedAt.Format(time.RFC3339)).
			Build(),
	)
}

// GetStreamInfo reads the projected stream metadata for one user. known is
// false when streamTitleField is absent (mirrors GetStreamLive's "live"
// check), letting the caller escalate instead of assuming an empty/offline
// stream. The six fields are fetched as individual HGETs in one DoMulti round
// trip (the GetCommand pattern) rather than one HMGET, because HMGET collapses
// a missing field and a genuinely empty one to the same "" and known needs to
// tell those apart.
func (v *Store) GetStreamInfo(ctx context.Context, userID string) (StreamInfo, bool, error) {
	defer segment(ctx, "HGET")()

	key, err := streamUserKey(userID)
	if err != nil {
		return StreamInfo{}, false, err
	}

	res := v.client.DoMulti(ctx,
		v.client.B().Hget().Key(key).Field(streamTitleField).Build(),
		v.client.B().Hget().Key(key).Field(streamGameField).Build(),
		v.client.B().Hget().Key(key).Field(streamViewersField).Build(),
		v.client.B().Hget().Key(key).Field(streamPeakViewersField).Build(),
		v.client.B().Hget().Key(key).Field(streamStartedAtField).Build(),
		v.client.B().Hget().Key(key).Field(streamEndedAtField).Build(),
	)

	title, err := res[0].ToString()
	if err != nil {
		if valkey.IsValkeyNil(err) {
			return StreamInfo{}, false, nil
		}
		return StreamInfo{}, false, err
	}

	game, _ := res[1].ToString()
	viewers, _ := res[2].ToString()
	peak, _ := res[3].ToString()
	startedAt, _ := res[4].ToString()
	endedAt, _ := res[5].ToString()

	viewerCount, _ := strconv.Atoi(viewers)
	peakViewers, _ := strconv.Atoi(peak)
	started, _ := time.Parse(time.RFC3339, startedAt)
	ended, _ := time.Parse(time.RFC3339, endedAt)

	return StreamInfo{
		Title:       title,
		GameName:    game,
		ViewerCount: viewerCount,
		PeakViewers: peakViewers,
		StartedAt:   started,
		EndedAt:     ended,
	}, true, nil
}

// StreamCounters is a snapshot of the counters an Overview panel diffs
// against to show per-stream deltas (this stream's messages/answers/mod
// actions, not the lifetime totals the fleet log already tracks).
type StreamCounters struct {
	Messages   int64
	Answered   int64
	ModActions int64
}

// SetStreamCounterBaseline projects the counter values observed at the start
// of the current stream, so a later read can subtract this snapshot to get a
// per-stream delta.
func (v *Store) SetStreamCounterBaseline(ctx context.Context, userID string, b StreamCounters) error {
	defer segment(ctx, "HSET")()

	key, err := streamUserKey(userID)
	if err != nil {
		return err
	}

	return v.pipelineWithTTL(ctx, key, DefaultTTL,
		v.client.B().Hset().
			Key(key).
			FieldValue().
			FieldValue(streamCtrMessagesField, strconv.FormatInt(b.Messages, 10)).
			FieldValue(streamCtrAnsweredField, strconv.FormatInt(b.Answered, 10)).
			FieldValue(streamCtrModActionField, strconv.FormatInt(b.ModActions, 10)).
			Build(),
	)
}

// GetStreamCounterBaseline reads the counter snapshot taken at stream start.
// known is false when streamCtrMessagesField is absent.
//
// This read is pinned to v.primary (pkg/valkey/routing.go's Primary wrapper):
// a dashboard opened right as a stream starts can race the baseline write
// against the node-local replica that plain Do would hit, so an
// under-replicated read would show a wrong (non-zero) delta for the first
// few seconds of every stream. Primary trades that staleness window for a
// Sentinel round trip on this one read.
func (v *Store) GetStreamCounterBaseline(ctx context.Context, userID string) (StreamCounters, bool, error) {
	defer segment(ctx, "HGET")()

	key, err := streamUserKey(userID)
	if err != nil {
		return StreamCounters{}, false, err
	}

	res := v.primary.DoMulti(ctx,
		v.primary.B().Hget().Key(key).Field(streamCtrMessagesField).Build(),
		v.primary.B().Hget().Key(key).Field(streamCtrAnsweredField).Build(),
		v.primary.B().Hget().Key(key).Field(streamCtrModActionField).Build(),
	)

	messages, err := res[0].ToString()
	if err != nil {
		if valkey.IsValkeyNil(err) {
			return StreamCounters{}, false, nil
		}
		return StreamCounters{}, false, err
	}

	answered, _ := res[1].ToString()
	modActions, _ := res[2].ToString()

	msgs, _ := strconv.ParseInt(messages, 10, 64)
	ans, _ := strconv.ParseInt(answered, 10, 64)
	mods, _ := strconv.ParseInt(modActions, 10, 64)

	return StreamCounters{Messages: msgs, Answered: ans, ModActions: mods}, true, nil
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

	projected, err = markerProjected(res[2])
	if err != nil {
		return CommandView{}, false, false, err
	}

	// Direct hit: the typed name is a command's own field.
	view, found, err = decodeJSONField[CommandView](res[0])
	if err != nil {
		return CommandView{}, false, projected, err
	}
	if found {
		return view, true, true, nil
	}

	// Alias hit: the typed name points at another command's field, read next.
	return v.resolveAlias(ctx, key, res[1], projected)
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

	view, found, err := decodeJSONField[CommandView](v.client.Do(ctx, v.client.B().Hget().Key(key).Field(commandFieldPrefix+primary).Build()))
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

// GetModules returns the user's module rows keyed by name. By-name is the shape
// every consumer actually wants: the pipeline indexes it once per chat line and
// the single-row readers (clip, timers, loyalty, followage) pick one entry out
// of it. This used to flatten the map below into a []ModuleView, which every
// caller then re-scanned or rebuilt into a map — a map build per chat message
// for a map this function had already built. Ordering was never a contract to
// break: the slice was emitted in Go map iteration order, and the one caller
// whose wire reply is still a list (the projector dashboard RPC, which converts
// with ModuleList) feeds a console that renders from its own static
// MODULE_CATALOG and looks module state up by name.
func (v *Store) GetModules(ctx context.Context, userID uint64) (map[string]ModuleView, bool, error) {
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
	// Sized from the hash: two fields (enabled + config) per module, plus the
	// section markers, so len(fields)/2 is a close upper bound that skips the
	// rehash the zero-sized literal paid on every read.
	byName := make(map[string]ModuleView, len(fields)/2)
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

	return byName, projected, nil
}

// ModuleList flattens a by-name module map into the list shape the projector's
// dashboard RPC reply carries on the wire. It lives at that one boundary on
// purpose: the reply contract is a list, while every read path wants the map.
// Order is unspecified, as it was before GetModules stopped flattening.
func ModuleList(byName map[string]ModuleView) []ModuleView {
	out := make([]ModuleView, 0, len(byName))
	for _, mod := range byName {
		out = append(out, mod)
	}
	return out
}

// ModuleMap keys a module list by name. Used where a list arrives from outside
// the store and has to be turned into the read shape: the projector RPC
// fallback in Client.Modules, and test readers that spell their fixtures as a
// list.
func ModuleMap(list []ModuleView) map[string]ModuleView {
	byName := make(map[string]ModuleView, len(list))
	for _, mod := range list {
		byName[mod.Name] = mod
	}
	return byName
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
