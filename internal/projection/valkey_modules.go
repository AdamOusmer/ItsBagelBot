// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package projection

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

const (
	moduleFieldPrefix  = "module:"
	modulesMarkerField = "modules:projected"
)

func (v *Store) SetModule(ctx context.Context, userID uint64, mod ModuleView) error {

	defer segment(ctx, "HSET")()

	key := cache.UserKey(settingsKeyPrefix, userID)
	configField := "module:" + mod.Name + ":config"

	fields := v.client.B().Hset().
		Key(key).
		FieldValue().
		FieldValue("module:"+mod.Name+":enabled", utils.BoolField(mod.IsEnabled))

	fields = fields.FieldValue(configField, string(mod.Configs))
	return v.pipelineWithTTL(ctx, key, DefaultTTL, fields.Build())
}

func (v *Store) SetModules(ctx context.Context, userID uint64, modules []ModuleView) error {
	return v.SetModulesWithTTL(ctx, userID, modules, DefaultTTL)
}

func (v *Store) SetModulesWithTTL(ctx context.Context, userID uint64, modules []ModuleView, ttl time.Duration) error {
	defer segment(ctx, "HSET")()

	rows := make([][2]string, 0, 2*len(modules))
	for _, mod := range modules {
		rows = append(rows, [2]string{"module:" + mod.Name + ":enabled", utils.BoolField(mod.IsEnabled)})
		if len(mod.Configs) > 0 {
			rows = append(rows, [2]string{"module:" + mod.Name + ":config", string(mod.Configs)})
		}
	}
	return v.replaceSection(ctx, userID, sectionWrite{
		prefixes: []string{moduleFieldPrefix},
		marker:   modulesMarkerField,
		ttl:      ttl,
		rows:     rows,
	})
}

func (v *Store) GetModules(ctx context.Context, userID uint64) (map[string]ModuleView, bool, error) {
	defer segment(ctx, "HGETALL")()

	key := cache.UserKey(settingsKeyPrefix, userID)
	fields, err := v.client.Do(ctx, v.client.B().Hgetall().Key(key).Build()).AsStrMap()
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
