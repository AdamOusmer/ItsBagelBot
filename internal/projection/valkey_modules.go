// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package projection

import (
	"context"
	"strconv"
	"strings"
	"time"

	contract "ItsBagelBot/internal/domain/rpc/projection"
	"ItsBagelBot/internal/utils"
	"ItsBagelBot/internal/watchtime"
	"ItsBagelBot/pkg/cache"
	"ItsBagelBot/pkg/codec"
	"github.com/valkey-io/valkey-go"
)

type ModuleView = contract.ModuleView

const (
	moduleFieldPrefix  = "module:"
	modulesMarkerField = "modules:projected"
)

// moduleHydrationSeed is intentionally additive. A hydration reply is a
// snapshot that may have been fetched before a newer ModuleChanged event. It
// updates only rows whose revision is at least the row already projected and
// leaves rows absent from the snapshot alone: the module service has no
// deletion event, so absence cannot safely mean deletion. Marker and rows are
// committed by this one script. Loyalty's revision survives settings expiry;
// a known missing loyalty row leaves hydration incomplete for a fresh fetch.
const moduleHydrationSeed = `-- module hydration revision seed
if redis.call('HGET',KEYS[2],'deleted') == '1' then return 0 end
local instance=redis.call('HGET',KEYS[2],'instance')
local ttl=tonumber(ARGV[1]); local n=tonumber(ARGV[2]); local p=3
if instance then
 for i=1,n do local start=3+(i-1)*5; if ARGV[start] == 'loyalty' and instance ~= ARGV[start+4] then return 0 end end
end
local warm=tonumber(redis.call('HGET',KEYS[1],'module:loyalty:revision') or '-1')
if instance and warm>=0 and redis.call('HGET',KEYS[1],'module:loyalty:account_created_at')==instance then
 local known=tonumber(redis.call('HGET',KEYS[2],'loyalty_revision') or '-1')
 redis.call('HSET',KEYS[2],'loyalty_revision',string.format('%.0f',math.max(warm,known)))
end
for i=1,n do
 local name=ARGV[p]; local incoming=tonumber(ARGV[p+1]); local current=tonumber(redis.call('HGET',KEYS[1],'module:'..name..':revision') or '-1')
 if name == 'loyalty' then
  current=math.max(current,tonumber(redis.call('HGET',KEYS[2],'loyalty_revision') or '-1'))
  if current>=0 then redis.call('HSET',KEYS[2],'loyalty_revision',string.format('%.0f',current)) end
 end
 if incoming>=current and (name ~= 'loyalty' or not instance or instance == ARGV[p+4]) then
  if name == 'loyalty' then redis.call('HSET',KEYS[2],'loyalty_revision',ARGV[p+1]) end
  if name == 'loyalty' and (redis.call('HGET',KEYS[1],'module:loyalty:enabled') ~= ARGV[p+2] or redis.call('HGET',KEYS[1],'module:loyalty:config') ~= ARGV[p+3]) then redis.call('HINCRBY',KEYS[2],'epoch',1) end
  redis.call('HSET',KEYS[1],'module:'..name..':revision',ARGV[p+1],'module:'..name..':enabled',ARGV[p+2],'module:'..name..':config',ARGV[p+3],'module:'..name..':account_created_at',ARGV[p+4])
 end
 p=p+5
end
local missingKnownLoyalty=redis.call('HEXISTS',KEYS[2],'loyalty_revision')==1 and redis.call('HEXISTS',KEYS[1],'module:loyalty:enabled')==0
if missingKnownLoyalty then redis.call('HDEL',KEYS[1],'modules:projected') else redis.call('HSET',KEYS[1],'modules:projected','1') end
local existing=redis.call('TTL',KEYS[1]); if existing<0 or existing<ttl then redis.call('EXPIRE',KEYS[1],ttl) end
return 1`

// moduleRevisionGate updates the three fields of one module atomically only
// when the incoming persisted revision is not older than the stored revision.
// Loyalty also fences against its persistent account-scoped revision so a
// delayed bus event cannot reintroduce older config after settings expiry.
const moduleRevisionGate = `-- module revision gate
if redis.call('HGET',KEYS[2],'deleted') == '1' then return 0 end
local instance=redis.call('HGET',KEYS[2],'instance')
if ARGV[3] == 'module:loyalty:enabled' and instance and instance ~= ARGV[8] then return 0 end
local current=tonumber(redis.call('HGET',KEYS[1],ARGV[1]) or '-1')
local incoming=tonumber(ARGV[2])
if ARGV[3] == 'module:loyalty:enabled' then
 current=math.max(current,tonumber(redis.call('HGET',KEYS[2],'loyalty_revision') or '-1'))
 if current>=0 then redis.call('HSET',KEYS[2],'loyalty_revision',string.format('%.0f',current)) end
end
if incoming < current then return 0 end
if ARGV[3] == 'module:loyalty:enabled' then redis.call('HSET',KEYS[2],'loyalty_revision',ARGV[2]) end
if ARGV[3] == 'module:loyalty:enabled' and (redis.call('HGET',KEYS[1],ARGV[3]) ~= ARGV[4] or redis.call('HGET',KEYS[1],ARGV[5]) ~= ARGV[6]) then redis.call('HINCRBY',KEYS[2],'epoch',1) end
redis.call('HSET',KEYS[1],ARGV[1],ARGV[2],ARGV[3],ARGV[4],ARGV[5],ARGV[6],string.gsub(ARGV[3],':enabled$',':account_created_at'),ARGV[8])
redis.call('EXPIRE',KEYS[1],ARGV[7],'NX')
redis.call('EXPIRE',KEYS[1],ARGV[7],'GT')
return 1`

// SetModule projects one module row of one user. It deliberately does NOT set
// the modules:projected marker: a single-row event landing on a cold hash must
// not make a partial module list read as complete. Only the full-section
// writes (SetModules / SetModulesWithTTL) mark the section projected; until
// one runs, readers fall through to the projector RPC, whose miss path
// hydrates the full list.
func (v *Store) SetModule(ctx context.Context, userID uint64, mod ModuleView) error {

	defer segment(ctx, "HSET")()
	if mod.Name == "loyalty" && mod.AccountCreatedAt > 0 {
		restored, err := v.RestoreAccount(ctx, userID, mod.AccountCreatedAt)
		if err != nil || !restored {
			return err
		}
	}

	key := cache.UserKey(settingsKeyPrefix, userID)
	configField := "module:" + mod.Name + ":config"

	revisionField := "module:" + mod.Name + ":revision"

	// Always write the config field, empty string and all, rather than
	// skipping the write when the config is cleared. Skipping it left the
	// PREVIOUS config in module:<name>:config and GetModules kept serving it
	// forever: a module is the only section whose one logical row spans two
	// hash fields, so it is the only one where an omitted write is not an
	// overwrite.
	//
	// Rejected HSET-then-HDEL in one pipelineWithTTL: DoMulti pipelines, it
	// does not open a transaction, so a concurrent HGETALL could land between
	// the two and read the new enabled flag beside the stale config — the very
	// state this fixes, made transient instead of permanent. Rejected MULTI/
	// EXEC over a dedicated connection for the same pair: it borrows a
	// connection per cleared config and still has to leave expiryCommands
	// outside the transaction. One HSET carrying both fields is atomic by
	// being one command.
	//
	// An empty field reads back exactly like an absent one: GetModules assigns
	// the raw value straight to Configs, so both yield a zero-length blob, and
	// nothing anywhere does HEXISTS on a config field. The stray empty field
	// costs a few bytes until the projection hash expires.
	cmd := v.primary.B().Eval().Script(moduleRevisionGate).Numkeys(2).Key(key, watchtime.AdmissionKey(userID)).
		Arg(revisionField).Arg(strconv.Itoa(mod.Revision)).
		Arg("module:" + mod.Name + ":enabled").Arg(utils.BoolField(mod.IsEnabled)).
		Arg(configField).Arg(string(mod.Configs)).
		Arg(strconv.Itoa(int(DefaultTTL / time.Second))).Arg(strconv.FormatInt(mod.AccountCreatedAt, 10)).Build()
	return v.primary.Do(ctx, cmd).Error()
}

func (v *Store) SetModules(ctx context.Context, userID uint64, modules []ModuleView) error {
	return v.SetModulesWithTTL(ctx, userID, modules, DefaultTTL)
}

// SetModulesWithTTL merges a complete snapshot by revision and keeps the hash
// for at least ttl. An empty list marks a cold section as projected, without
// erasing rows from events newer than the snapshot.
func (v *Store) SetModulesWithTTL(ctx context.Context, userID uint64, modules []ModuleView, ttl time.Duration) error {
	defer segment(ctx, "HSET")()
	// User and module hydration run concurrently. Either canonical section
	// can establish the incarnation before writing, so the other's first
	// restoration cannot clear an already committed loyalty configuration.
	for _, mod := range modules {
		if mod.Name == "loyalty" && mod.AccountCreatedAt > 0 {
			restored, err := v.RestoreAccount(ctx, userID, mod.AccountCreatedAt)
			if err != nil || !restored {
				return err
			}
		}
	}

	args := make([]string, 0, 2+5*len(modules))
	seconds := int64(ttl / time.Second)
	if seconds < 1 {
		seconds = 1
	}
	args = append(args, strconv.FormatInt(seconds, 10), strconv.Itoa(len(modules)))
	for _, mod := range modules {
		args = append(args, mod.Name, strconv.Itoa(mod.Revision), utils.BoolField(mod.IsEnabled), string(mod.Configs), strconv.FormatInt(mod.AccountCreatedAt, 10))
	}
	key := cache.UserKey(settingsKeyPrefix, userID)
	return v.primary.Do(ctx, v.primary.B().Eval().Script(moduleHydrationSeed).Numkeys(2).Key(key, watchtime.AdmissionKey(userID)).Arg(args...).Build()).Error()
}

func (v *Store) GetModules(ctx context.Context, userID uint64) (map[string]ModuleView, bool, error) {
	return v.getModules(ctx, v.client, userID)
}

// GetModulesPrimary is for authority checks and hydration read-back. Ordinary
// worker reads keep using the node-local projection through GetModules.
func (v *Store) GetModulesPrimary(ctx context.Context, userID uint64) (map[string]ModuleView, bool, error) {
	return v.getModules(ctx, v.primary, userID)
}

func (v *Store) getModules(ctx context.Context, client valkey.Client, userID uint64) (map[string]ModuleView, bool, error) {
	defer segment(ctx, "HGETALL")()

	key := cache.UserKey(settingsKeyPrefix, userID)
	fields, err := client.Do(ctx, client.B().Hgetall().Key(key).Build()).AsStrMap()
	if err != nil {
		return nil, false, err
	}

	projected := fields[modulesMarkerField] == "1"
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
		case "revision":
			mod.Revision, _ = strconv.Atoi(value)
		case "account_created_at":
			mod.AccountCreatedAt, _ = strconv.ParseInt(value, 10, 64)
		}
		byName[name] = mod
	}

	return byName, projected, nil
}

func ModuleList(byName map[string]ModuleView) []ModuleView {
	out := make([]ModuleView, 0, len(byName))
	for _, mod := range byName {
		out = append(out, mod)
	}
	return out
}

func ModuleMap(list []ModuleView) map[string]ModuleView {
	byName := make(map[string]ModuleView, len(list))
	for _, mod := range list {
		byName[mod.Name] = mod
	}
	return byName
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

func (v *Store) GetModule(ctx context.Context, userID uint64, name string) (ModuleView, bool, error) {
	defer segment(ctx, "HMGET")()

	key := cache.UserKey(settingsKeyPrefix, userID)
	fields, err := v.client.Do(ctx, v.client.B().Hmget().Key(key).
		Field("module:"+name+":enabled").
		Field("module:"+name+":config").
		Field("module:"+name+":account_created_at").
		Build()).ToArray()
	if err != nil {
		return ModuleView{}, false, err
	}
	if len(fields) < 2 {
		return ModuleView{}, false, nil
	}
	enabled, enabledErr := fields[0].ToString()
	cfg, cfgErr := fields[1].ToString()
	if enabledErr != nil && cfgErr != nil {
		return ModuleView{}, false, nil
	}
	var instance int64
	if len(fields) > 2 {
		raw, _ := fields[2].ToString()
		instance, _ = strconv.ParseInt(raw, 10, 64)
	}
	return ModuleView{Name: name, IsEnabled: enabled == "1", Configs: codec.RawMessage(cfg), AccountCreatedAt: instance}, true, nil
}
