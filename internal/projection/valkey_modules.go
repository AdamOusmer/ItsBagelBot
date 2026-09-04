// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package projection

// Module projection: the module:<name>:enabled / :config rows, their
// modules:projected marker and the by-name read shape every consumer indexes.
// Split from valkey.go so the module section reads as one file beside the
// command, stream and fetch sections (the valkey_fetch.go precedent).

import (
	"context"
	"strings"
	"time"

	contract "ItsBagelBot/internal/domain/rpc/projection"
	"ItsBagelBot/internal/utils"
	"ItsBagelBot/pkg/cache"
	"ItsBagelBot/pkg/codec"
)

type ModuleView = contract.ModuleView

// SetModule projects one module row of one user. It deliberately does NOT set
// the modules:projected marker: a single-row event landing on a cold hash must
// not make a partial module list read as complete. Only the full-section
// writes (SetModules / SetModulesWithTTL) mark the section projected; until
// one runs, readers fall through to the projector RPC, whose miss path
// hydrates the full list.
func (v *Store) SetModule(ctx context.Context, userID uint64, mod ModuleView) error {

	defer segment(ctx, "HSET")()

	key := cache.UserKey(settingsKeyPrefix, userID)
	configField := "module:" + mod.Name + ":config"

	fields := v.client.B().Hset().
		Key(key).
		FieldValue().
		FieldValue("module:"+mod.Name+":enabled", utils.BoolField(mod.IsEnabled))

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
	// costs a few bytes until the next full-section write sweeps the prefix.
	fields = fields.FieldValue(configField, string(mod.Configs))
	return v.pipelineWithTTL(ctx, key, DefaultTTL, fields.Build())
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

// GetModule reads one module row without HGETALL. A row is found when
// either of its fields exists: keying off the enabled flag alone read a row
// whose config landed before its toggle as absent, which GetModules
// (HGETALL-based) never did. Not-found means the Discord live announcer
// treats the user as "not connected".
func (v *Store) GetModule(ctx context.Context, userID uint64, name string) (ModuleView, bool, error) {
	defer segment(ctx, "HMGET")()

	key := cache.UserKey(settingsKeyPrefix, userID)
	fields, err := v.client.Do(ctx, v.client.B().Hmget().Key(key).
		Field("module:"+name+":enabled").
		Field("module:"+name+":config").
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
	return ModuleView{Name: name, IsEnabled: enabled == "1", Configs: codec.RawMessage(cfg)}, true, nil
}
