// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"time"

	"ItsBagelBot/app/twitch/sesame/engine/draw"
	"ItsBagelBot/app/twitch/sesame/engine/eligibility"
	"ItsBagelBot/internal/projection"
	"ItsBagelBot/pkg/codec"
)

const GiveawayModuleName = "giveaway"

type GiveawayModuleConfig struct {
	DefaultPrize   string           `json:"defaultPrize"`
	Winners        codec.Number     `json:"winners"`
	WindowSeconds  codec.Number     `json:"windowSeconds"`
	ClaimSeconds   codec.Number     `json:"claimSeconds"`
	RerollNoShows  giveawayToggle   `json:"rerollNoShows"`
	DefaultPreset  string           `json:"defaultPreset"`
	Presets        []GiveawayPreset `json:"presets"`
	OpenedEnabled  giveawayToggle   `json:"openedEnabled"`
	WonEnabled     giveawayToggle   `json:"wonEnabled"`
	NoShowEnabled  giveawayToggle   `json:"noShowEnabled"`
	RefusalEnabled refusalToggle    `json:"refusalEnabled"`
	OpenedMessage  string           `json:"openedMessage"`
	WonMessage     string           `json:"wonMessage"`
}

type GiveawayPreset struct {
	Name  string            `json:"name"`
	Rules eligibility.Rules `json:"rules"`
}

var giveawayPresetName = regexp.MustCompile(`^[a-z0-9-]+$`)

func (c GiveawayModuleConfig) Preset(name string) (GiveawayPreset, bool) {
	name = strings.ToLower(strings.TrimSpace(name))
	for _, preset := range c.Presets {
		if preset.Name == name {
			return preset, true
		}
	}
	return GiveawayPreset{}, false
}

func (c GiveawayModuleConfig) LaunchPreset(name string) (GiveawayPreset, bool) {
	name = strings.TrimSpace(name)
	if name == "" {
		name = strings.TrimSpace(c.DefaultPreset)
	}
	if name == "" {
		return GiveawayPreset{}, true
	}
	return c.Preset(name)
}

func (c GiveawayModuleConfig) Stock() int {
	n, err := c.Winners.Int64()
	if err != nil || n <= 0 {
		return draw.Pins.GiveawayDefaultWinners
	}
	return int(min(n, int64(draw.Pins.MaxWinners)))
}

func (c GiveawayModuleConfig) EntryWindow() time.Duration {
	seconds, err := c.WindowSeconds.Int64()
	if err != nil || seconds <= 0 {
		return draw.Pins.GiveawayEntryWindow
	}
	return time.Duration(min(seconds, int64(draw.Pins.GiveawayMaxEntryWindow/time.Second))) * time.Second
}

func (c GiveawayModuleConfig) ClaimWindow() time.Duration {
	seconds, err := c.ClaimSeconds.Int64()
	if err != nil || seconds < 0 {
		return draw.Pins.ClaimWindowDefault
	}
	if seconds == 0 {
		return 0
	}
	return time.Duration(min(max(seconds, int64(draw.Pins.ClaimWindowMin/time.Second)),
		int64(draw.Pins.ClaimWindowMax/time.Second))) * time.Second
}

func (c *GiveawayModuleConfig) clampPresets() {
	normalized := c.Presets[:0]
	for _, preset := range c.Presets[:min(len(c.Presets), draw.Pins.GiveawayMaxPresets)] {
		preset.Name = strings.ToLower(strings.TrimSpace(preset.Name))
		if len(preset.Name) > draw.Pins.GiveawayPresetNameMax || !giveawayPresetName.MatchString(preset.Name) {
			continue
		}
		preset.Rules = preset.Rules.Clamp()
		normalized = append(normalized, preset)
	}
	c.Presets = normalized
}

type giveawayToggle bool

func (v *giveawayToggle) UnmarshalJSON(raw []byte) error {
	switch string(raw) {
	case "true", `"true"`, `"on"`:
		*v = true
	case "false", `"false"`, `"off"`:
		*v = false
	case "null":
	default:
		return errors.New("giveaway: invalid toggle setting")
	}
	return nil
}

type refusalToggle giveawayToggle

func (v *refusalToggle) UnmarshalJSON(raw []byte) error {
	if err := (*giveawayToggle)(v).UnmarshalJSON(raw); err != nil {
		return errors.New("giveaway: invalid refusalEnabled setting")
	}
	return nil
}

func ReadGiveawayConfig(ctx context.Context, proj projection.Reader, channel uint64) (GiveawayModuleConfig, error) {
	cfg, _, err := readGiveawayModule(ctx, proj, channel)
	return cfg, err
}

func defaultGiveawayConfig() GiveawayModuleConfig {
	return GiveawayModuleConfig{OpenedEnabled: true, WonEnabled: true, NoShowEnabled: true,
		RefusalEnabled: refusalToggle(eligibility.Limits.RefusalEnabled)}
}

type giveawayPrizeSeed struct {
	ClaimPrize string `json:"claimPrize"`
}

func readGiveawayModule(ctx context.Context, proj projection.Reader, channel uint64) (GiveawayModuleConfig, ModuleState, error) {
	cfg := defaultGiveawayConfig()
	view, state, err := ModuleLookup{Proj: proj, BroadcasterID: channel, Name: GiveawayModuleName, Absent: ModuleOff}.Resolve(ctx)
	switch {
	case err != nil:
		return cfg, state, err
	case view.Name == "" && len(view.Configs) == 0:
		cfg.DefaultPrize = readRaffleConfig[giveawayPrizeSeed](ctx, proj, channel).ClaimPrize
	case len(view.Configs) > 0:
		err = codec.Unmarshal(view.Configs, &cfg)
		cfg.clampPresets()
	}
	return cfg, state, err
}
