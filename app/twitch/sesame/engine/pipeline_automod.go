// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"ItsBagelBot/app/twitch/sesame/automod"
	"ItsBagelBot/app/twitch/sesame/confcache"
	"ItsBagelBot/internal/projection"
)

const automodModuleName = "automod"

func moduleBeta(reg *Registry, name string) bool {
	for _, m := range reg.For(chatType) {
		if m.Name == name {
			return m.Beta
		}
	}
	return false
}

func automodConfigFrom(views map[string]projection.ModuleView, locked bool) *automod.Config {
	mv, ok := views[automodModuleName]
	if !ok && !locked {
		return nil
	}
	var cfg *automod.Config
	if ok {
		cfg = automodConfigs.Get(mv.Configs, parseAutomodConfig)
	}
	if locked || !mv.IsEnabled {
		return disabledConfig(cfg)
	}
	return cfg
}

var automodConfigs = confcache.New[*automod.Config]()

func parseAutomodConfig(raw []byte) *automod.Config { return automod.ParseConfig(raw) }

// Copy: cfg is shared through confcache by every channel with the same blob.
func disabledConfig(cfg *automod.Config) *automod.Config {
	if cfg == nil {
		return &automod.Config{Disabled: true}
	}
	c := *cfg
	c.Disabled = true
	return &c
}
