package projection

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"ItsBagelBot/internal/domain/event/data"
	contract "ItsBagelBot/internal/domain/rpc/projection"
	"ItsBagelBot/internal/utils"
	"ItsBagelBot/pkg/cache"

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
)

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
}

// NewStore creates a new Store instance using the provided Valkey client.
func NewStore(client valkey.Client) *Store {
	return &Store{client: client}
}

// SetUser projects the tier status, active flag, ban flag and UI locale of one
// user. An empty locale leaves the projected locale untouched (see
// SetUserWithTTL).
func (v *Store) SetUser(ctx context.Context, userID uint64, status string, isActive bool, banned bool, locale string) error {
	return v.SetUserWithTTL(ctx, userID, status, isActive, banned, locale, DefaultTTL)
}

// SetUserWithTTL projects the user fields and keeps the hash for at least ttl.
// locale is written only when non-empty: cold-read write-backs (the status RPC)
// and older events that carry no locale must not overwrite a locale the full
// user projection already set.
func (v *Store) SetUserWithTTL(ctx context.Context, userID uint64, status string, isActive bool, banned bool, locale string, ttl time.Duration) error {

	defer segment(ctx, "HSET")()

	key := cache.UserKey(settingsKeyPrefix, userID)

	fields := v.client.B().Hset().
		Key(key).
		FieldValue().
		FieldValue("status", status).
		FieldValue("active", utils.BoolField(isActive)).
		FieldValue("banned", utils.BoolField(banned))
	if locale != "" {
		fields = fields.FieldValue("locale", locale)
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

// SetModule projects one module row of one user.
func (v *Store) SetModule(ctx context.Context, userID uint64, name string, isEnabled bool, configs []byte) error {

	defer segment(ctx, "HSET")()

	key := cache.UserKey(settingsKeyPrefix, userID)

	fields := v.client.B().Hset().
		Key(key).
		FieldValue().
		FieldValue("modules:projected", "1").
		FieldValue("module:"+name+":enabled", utils.BoolField(isEnabled))

	if len(configs) > 0 {
		fields = fields.FieldValue("module:"+name+":config", string(configs))
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

	key := cache.UserKey(settingsKeyPrefix, userID)
	if err := v.clearProjectionFields(ctx, key, "module:"); err != nil {
		return err
	}

	fields := v.client.B().Hset().
		Key(key).
		FieldValue().
		FieldValue("modules:projected", "1")

	for _, mod := range modules {
		fields = fields.FieldValue("module:"+mod.Name+":enabled", utils.BoolField(mod.IsEnabled))
		if len(mod.Configs) > 0 {
			fields = fields.FieldValue("module:"+mod.Name+":config", string(mod.Configs))
		}
	}

	return v.pipelineWithTTL(ctx, key, ttl, fields.Build())
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

	old, _ := v.client.Do(ctx, v.client.B().Hget().Key(key).Field(field).Build()).ToString()
	var oldAliases []string
	if old != "" {
		var prev CommandView
		if json.Unmarshal([]byte(old), &prev) == nil {
			oldAliases = prev.Aliases
		}
	}

	cmds := make([]valkey.Completed, 0, 4)
	if len(oldAliases) > 0 {
		stale := make([]string, 0, len(oldAliases))
		for _, a := range oldAliases {
			stale = append(stale, aliasFieldPrefix+strings.ToLower(a))
		}
		cmds = append(cmds, v.client.B().Hdel().Key(key).Field(stale...).Build())
	}

	if dto.Deleted {
		cmds = append(cmds,
			v.client.B().Hdel().Key(key).Field(field).Build(),
			v.client.B().Hset().Key(key).FieldValue().FieldValue("commands:projected", "1").Build(),
		)
		cmds = append(cmds, v.expiryCommands(key, DefaultTTL)...)
		return v.pipeline(ctx, cmds...)
	}

	view := commandViewFromEvent(dto)
	body, err := json.Marshal(view)
	if err != nil {
		return err
	}

	set := v.client.B().Hset().Key(key).FieldValue().
		FieldValue("commands:projected", "1").
		FieldValue(field, string(body))
	for _, a := range view.Aliases {
		set = set.FieldValue(aliasFieldPrefix+strings.ToLower(a), name)
	}
	cmds = append(cmds, set.Build())
	cmds = append(cmds, v.expiryCommands(key, DefaultTTL)...)
	return v.pipeline(ctx, cmds...)
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

	pj, perr := res[2].ToString()
	if perr != nil && !valkey.IsValkeyNil(perr) {
		return CommandView{}, false, false, perr
	}
	projected = pj == "1"

	body, berr := res[0].ToString()
	if berr != nil && !valkey.IsValkeyNil(berr) {
		return CommandView{}, false, projected, berr
	}
	if berr == nil {
		if json.Unmarshal([]byte(body), &view) == nil {
			return view, true, true, nil
		}
	}

	primary, aerr := res[1].ToString()
	if aerr != nil && !valkey.IsValkeyNil(aerr) {
		return CommandView{}, false, projected, aerr
	}
	if aerr == nil && primary != "" {
		aliasBody, err := v.client.Do(ctx, v.client.B().Hget().Key(key).Field(commandFieldPrefix+primary).Build()).ToString()
		if err == nil && json.Unmarshal([]byte(aliasBody), &view) == nil {
			return view, true, true, nil
		}
	}

	return CommandView{}, false, projected, nil
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
		body, err := json.Marshal(cmd)
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
			projected = true
		case "config":
			mod.Configs = json.RawMessage(value)
			projected = true
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

	projected := fields["commands:projected"] == "1"
	out := make([]CommandView, 0)
	for field, value := range fields {
		name, ok := strings.CutPrefix(field, "command:")
		if !ok || name == "" {
			continue
		}
		var cmd CommandView
		if err := json.Unmarshal([]byte(value), &cmd); err != nil {
			continue
		}
		out = append(out, cmd)
		projected = true
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
