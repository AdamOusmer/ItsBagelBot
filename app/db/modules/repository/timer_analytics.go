// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"ItsBagelBot/app/db/modules/ent"
	"ItsBagelBot/internal/analytics"
	catalog "ItsBagelBot/internal/domain/analytics"
	"ItsBagelBot/pkg/codec"
)

type timerOptions struct {
	Enabled      bool   `json:"enabled"`
	MinChatLines int    `json:"minChatLines"`
	MaxFires     int    `json:"maxFiresPerStream"`
	EndsAt       string `json:"endsAt"`
	AllowOffline bool   `json:"allowOffline"`
}

func tallyTimers(t analytics.Tally, row *ent.Modules) {
	if row.Name != "timers" || !row.IsEnabled {
		return
	}
	var cfg struct {
		Timers []timerOptions `json:"timers"`
	}
	if codec.Unmarshal(row.Configs, &cfg) != nil {
		return
	}
	for _, timer := range cfg.Timers {
		if timer.Enabled {
			timer.tally(t)
		}
	}
}

func (timer timerOptions) tally(t analytics.Tally) {
	opts := []struct {
		name string
		on   bool
	}{
		{"gated", timer.MinChatLines > 0},
		{"capped", timer.MaxFires > 0},
		{"ends", timer.EndsAt != ""},
		{"offline", timer.AllowOffline},
	}
	plain := true
	for _, opt := range opts {
		if opt.on {
			t.Add(catalog.TimersTotal, opt.name)
			plain = false
		}
	}
	if plain {
		t.Add(catalog.TimersTotal, "plain")
	}
}
