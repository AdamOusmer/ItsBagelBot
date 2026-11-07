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
	pkg_valkey "ItsBagelBot/pkg/valkey"

	"github.com/valkey-io/valkey-go"
)

type ModuleView = contract.ModuleView

const (
	moduleFieldPrefix  = "module:"
	modulesMarkerField = "modules:projected"
)

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
	keys := []string{key, watchtime.AdmissionKey(userID)}
	args := []string{revisionField, strconv.Itoa(mod.Revision), "module:" + mod.Name + ":enabled", utils.BoolField(mod.IsEnabled), configField, string(mod.Configs), strconv.Itoa(int(DefaultTTL / time.Second)), strconv.FormatInt(mod.AccountCreatedAt, 10)}
	return pkg_valkey.RunHashRevisionGateModule(ctx, v.primary, keys, args).Error()
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
	return pkg_valkey.RunHashSnapshotSeed(ctx, v.primary, []string{key, watchtime.AdmissionKey(userID)}, args).Error()
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
	key := cache.UserKey(settingsKeyPrefix, userID)
	fields, err := readModuleSnapshot(ctx, client, key)
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

// readModuleSnapshot keeps one atomic snapshot, with compatibility fallback only
// for servers or ACLs that cannot execute EVAL_RO. Transport failures propagate.
func readModuleSnapshot(ctx context.Context, client valkey.Client, key string) (map[string]string, error) {
	end := segment(ctx, "EVAL_RO")
	fields, err := pkg_valkey.RunHashSnapshotRead(ctx, client, key).AsStrMap()
	end()
	if moduleSnapshotNeedsFallback(err) {
		defer segment(ctx, "HGETALL")()
		return pkg_valkey.HashAll(ctx, client, key)
	}
	return fields, err
}

func moduleSnapshotNeedsFallback(err error) bool {
	if err == nil {
		return false
	}
	if strings.Contains(err.Error(), "unknown command") {
		return true
	}
	return strings.Contains(err.Error(), "NOPERM")
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
	fields, err := pkg_valkey.Exec1(ctx, v.client, pkg_valkey.HMGetCmd(v.client, key, "module:"+name+":enabled", "module:"+name+":config", "module:"+name+":account_created_at")).ToArray()
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
