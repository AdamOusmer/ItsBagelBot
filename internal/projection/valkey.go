package projection

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"

	"ItsBagelBot/internal/domain/event/data"
	contract "ItsBagelBot/internal/domain/rpc/projection"
	"ItsBagelBot/internal/utils"
	"ItsBagelBot/pkg/cache"

	"github.com/newrelic/go-agent/v3/newrelic"

	"github.com/valkey-io/valkey-go"
)

const settingsKeyPrefix = "settings:"

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

// SetUser projects the tier status, active flag and ban flag of one user.
func (v *Store) SetUser(ctx context.Context, userID uint64, status string, isActive bool, banned bool) error {

	defer segment(ctx, "HSET")()

	key := cache.UserKey(settingsKeyPrefix, userID)

	return v.pipeline(ctx,
		v.client.B().Hset().
			Key(key).
			FieldValue().
			FieldValue("status", status).
			FieldValue("active", utils.BoolField(isActive)).
			FieldValue("banned", utils.BoolField(banned)).
			Build(),
		v.client.B().Expire().Key(key).Seconds(24*60*60).Build(),
	)
}

// GetUser retrieves the tier status, active flag and ban flag of one user.
func (v *Store) GetUser(ctx context.Context, userID uint64) (string, bool, bool, error) {
	defer segment(ctx, "HGETALL")()

	key := cache.UserKey(settingsKeyPrefix, userID)

	res, err := v.client.Do(ctx, v.client.B().Hmget().Key(key).Field("status").Field("active").Field("banned").Build()).AsStrSlice()
	if err != nil {
		return "", false, false, err
	}

	if len(res) < 3 {
		return "", false, false, nil
	}

	status := res[0]
	active := res[1] == "1"
	banned := res[2] == "1"

	return status, active, banned, nil
}

// SetStreamLive projects Twitch's current live/offline signal for one user.
func (v *Store) SetStreamLive(ctx context.Context, userID uint64, live bool) error {

	defer segment(ctx, "HSET")()

	key := cache.UserKey(settingsKeyPrefix, userID)

	return v.pipeline(ctx,
		v.client.B().Hset().
			Key(key).
			FieldValue().
			FieldValue("live", utils.BoolField(live)).
			Build(),
		v.client.B().Expire().Key(key).Seconds(24*60*60).Build(),
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

	return v.pipeline(ctx,
		fields.Build(),
		v.client.B().Expire().Key(key).Seconds(24*60*60).Build(),
	)
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
	}
}

// SetModules projects a complete module list and records that an empty list is
// known data, not a cold Valkey miss.
func (v *Store) SetModules(ctx context.Context, userID uint64, modules []ModuleView) error {
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

	return v.pipeline(ctx,
		fields.Build(),
		v.client.B().Expire().Key(key).Seconds(24*60*60).Build(),
	)
}

// SetCommand projects one command row of one user.
func (v *Store) SetCommand(ctx context.Context, dto data.CommandChangedDTO) error {
	defer segment(ctx, "HSET")()

	key := cache.UserKey(settingsKeyPrefix, dto.UserID)
	field := "command:" + dto.Name

	if dto.Deleted {
		return v.pipeline(ctx,
			v.client.B().Hdel().Key(key).Field(field).Build(),
			v.client.B().Hset().
				Key(key).
				FieldValue().
				FieldValue("commands:projected", "1").
				Build(),
			v.client.B().Expire().Key(key).Seconds(24*60*60).Build(),
		)
	}

	body, err := json.Marshal(commandViewFromEvent(dto))
	if err != nil {
		return err
	}

	return v.pipeline(ctx,
		v.client.B().Hset().
			Key(key).
			FieldValue().
			FieldValue("commands:projected", "1").
			FieldValue(field, string(body)).
			Build(),
		v.client.B().Expire().Key(key).Seconds(24*60*60).Build(),
	)
}

// SetCommands projects a complete command list and records that an empty list is
// known data, not a cold Valkey miss.
func (v *Store) SetCommands(ctx context.Context, userID uint64, commands []CommandView) error {
	defer segment(ctx, "HSET")()

	key := cache.UserKey(settingsKeyPrefix, userID)
	if err := v.clearProjectionFields(ctx, key, "command:"); err != nil {
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
		fields = fields.FieldValue("command:"+cmd.Name, string(body))
	}

	return v.pipeline(ctx,
		fields.Build(),
		v.client.B().Expire().Key(key).Seconds(24*60*60).Build(),
	)
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

func (v *Store) clearProjectionFields(ctx context.Context, key string, prefix string) error {
	fields, err := v.client.Do(ctx, v.client.B().Hgetall().Key(key).Build()).AsStrMap()
	if err != nil {
		return err
	}

	stale := make([]string, 0, len(fields))
	for field := range fields {
		if strings.HasPrefix(field, prefix) {
			stale = append(stale, field)
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
