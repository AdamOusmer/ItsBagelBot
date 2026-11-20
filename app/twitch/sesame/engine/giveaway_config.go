// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"errors"

	"ItsBagelBot/app/twitch/sesame/engine/eligibility"
	"ItsBagelBot/internal/projection"
	"ItsBagelBot/pkg/codec"
)

const GiveawayModuleName = "giveaway"

type GiveawayModuleConfig struct {
	RefusalEnabled refusalToggle `json:"refusalEnabled"`
}

type refusalToggle bool

func (v *refusalToggle) UnmarshalJSON(raw []byte) error {
	switch string(raw) {
	case "true", `"true"`, `"on"`:
		*v = true
	case "false", `"false"`, `"off"`:
		*v = false
	case "null":
	default:
		return errors.New("giveaway: invalid refusalEnabled setting")
	}
	return nil
}

func ReadGiveawayConfig(ctx context.Context, proj projection.Reader, channel uint64) (GiveawayModuleConfig, error) {
	cfg := GiveawayModuleConfig{RefusalEnabled: refusalToggle(eligibility.Limits.RefusalEnabled)}
	view, _, err := ModuleLookup{Proj: proj, BroadcasterID: channel, Name: GiveawayModuleName, Absent: ModuleOff}.Resolve(ctx)
	if err != nil || len(view.Configs) == 0 {
		return cfg, err
	}
	err = codec.Unmarshal(view.Configs, &cfg)
	return cfg, err
}
