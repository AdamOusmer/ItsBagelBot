// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"context"
	"strconv"
	"strings"

	"ItsBagelBot/app/sesame/engine"
	"ItsBagelBot/app/sesame/module"
	ddiscord "ItsBagelBot/internal/domain/discord"
	"ItsBagelBot/internal/domain/outgress"
	"ItsBagelBot/pkg/codec"
)

const discordModuleName = "discord"

func discordCfg(c *module.Context) ddiscord.Config {
	var cfg ddiscord.Config
	_ = c.Decode(&cfg)
	return cfg
}

func emitDiscord(c *module.Context, channelID, text string, emit module.Emit) {
	if channelID == "" || strings.TrimSpace(text) == "" {
		return
	}
	emit(&module.Output{
		Type:          outgress.TypeDiscordChat,
		BroadcasterID: c.Env.BroadcasterUserID,
		ChannelID:     channelID,
		Text:          text,
	})
}

type discordRaid struct {
	FromBroadcasterUserName  string `json:"from_broadcaster_user_name"`
	FromBroadcasterUserLogin string `json:"from_broadcaster_user_login"`
	Viewers                  int    `json:"viewers"`
}

type discordGift struct {
	UserName          string `json:"user_name"`
	UserLogin         string `json:"user_login"`
	Total             int    `json:"total"`
	BroadcasterUserID string `json:"broadcaster_user_id"`
}

type discordCheer struct {
	UserName          string `json:"user_name"`
	UserLogin         string `json:"user_login"`
	Bits              int    `json:"bits"`
	BroadcasterUserID string `json:"broadcaster_user_id"`
}

type discordSubMsg struct {
	UserName          string `json:"user_name"`
	UserLogin         string `json:"user_login"`
	CumulativeMonths  int    `json:"cumulative_months"`
	BroadcasterUserID string `json:"broadcaster_user_id"`
}

func atoiDefault(s string, fallback int) int {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil || n <= 0 {
		return fallback
	}
	return n
}

func isMilestone(months int) bool {
	switch months {
	case 1, 6, 12, 24, 36, 48, 60, 72, 84, 96, 108, 120:
		return true
	}
	return months > 0 && months%12 == 0
}

// Discord copies raid / gift-bomb / milestone alerts into the connected
// guild. Go-live is NOT here: outgress binds twitch.ingress.event.stream
// directly so live posts never pass through sesame.
func Discord(_ engine.Deps) module.Module {
	m := module.NewModule(discordModuleName, module.KindOptIn)

	m.On("channel.raid", func(_ context.Context, c *module.Context, emit module.Emit) error {
		cfg := discordCfg(c)
		if !cfg.Connected() || !cfg.RaidOn() {
			return nil
		}
		var ev discordRaid
		if err := codec.Unmarshal(c.Env.Event, &ev); err != nil {
			return err
		}
		who := chatName(ev.FromBroadcasterUserName, ev.FromBroadcasterUserLogin)
		emitDiscord(c, cfg.AlertsChannel(), who+" is raiding with "+strconv.Itoa(ev.Viewers)+" viewers. Watch on Twitch.", emit)
		return nil
	})

	m.On("channel.subscription.gift", func(_ context.Context, c *module.Context, emit module.Emit) error {
		cfg := discordCfg(c)
		if !cfg.Connected() || !cfg.GiftOn() {
			return nil
		}
		var ev discordGift
		if err := codec.Unmarshal(c.Env.Event, &ev); err != nil {
			return err
		}
		if ev.Total < atoiDefault(cfg.GiftMin, 5) {
			return nil
		}
		who := chatName(ev.UserName, ev.UserLogin)
		emitDiscord(c, cfg.AlertsChannel(), who+" gifted "+strconv.Itoa(ev.Total)+" subs.", emit)
		return nil
	})

	m.On("channel.cheer", func(_ context.Context, c *module.Context, emit module.Emit) error {
		cfg := discordCfg(c)
		if !cfg.Connected() || !cfg.CheerOn() {
			return nil
		}
		var ev discordCheer
		if err := codec.Unmarshal(c.Env.Event, &ev); err != nil {
			return err
		}
		if ev.Bits < atoiDefault(cfg.CheerMin, 1000) {
			return nil
		}
		who := chatName(ev.UserName, ev.UserLogin)
		emitDiscord(c, cfg.AlertsChannel(), who+" cheered "+strconv.Itoa(ev.Bits)+" bits.", emit)
		return nil
	})

	m.On("channel.subscription.message", func(_ context.Context, c *module.Context, emit module.Emit) error {
		cfg := discordCfg(c)
		if !cfg.Connected() || !cfg.SubMilestoneOn() {
			return nil
		}
		var ev discordSubMsg
		if err := codec.Unmarshal(c.Env.Event, &ev); err != nil {
			return err
		}
		if !isMilestone(ev.CumulativeMonths) {
			return nil
		}
		who := chatName(ev.UserName, ev.UserLogin)
		emitDiscord(c, cfg.AlertsChannel(), who+" hit "+strconv.Itoa(ev.CumulativeMonths)+" months.", emit)
		return nil
	})

	return m.Build()
}
