// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"strings"

	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/pkg/codec"
)

func triggerDisabled(raw codec.RawMessage, trigger module.TriggerName) bool {
	if len(raw) == 0 || trigger == "" {
		return false
	}
	var cfg struct {
		DisabledTriggers string `json:"disabledTriggers"`
	}
	if err := codec.Unmarshal(raw, &cfg); err != nil || cfg.DisabledTriggers == "" {
		return false
	}
	for entry := range strings.SplitSeq(cfg.DisabledTriggers, ",") {
		if triggerMatches(entry, trigger) {
			return true
		}
	}
	return false
}

func triggerMatches(raw string, trigger module.TriggerName) bool {
	entry := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(raw), "!"))
	return entry != "" && strings.EqualFold(entry, string(trigger))
}
