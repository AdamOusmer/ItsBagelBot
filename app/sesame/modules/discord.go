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
	if channelID == "" {
		return
	}
	if strings.TrimSpace(text) == "" {
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
	if err != nil {
		return fallback
	}
	if n <= 0 {
		return fallback
	}
	return n
}

func isMilestone(months int) bool {
	switch months {
	case 1, 6, 12, 24, 36, 48, 60, 72, 84, 96, 108, 120:
		return true
	}
	if months <= 0 {
		return false
	}
	return months%12 == 0
}

// discordCopy is one alert copied into the guild: enabled reads the module
// toggle, line renders the event (an empty line skips the copy). Every
// handler shares the connected/toggle/decode preamble through handle.
type discordCopy[T any] struct {
	enabled func(ddiscord.Config) bool
	line    func(ddiscord.Config, T) string
}

func (h discordCopy[T]) handle(_ context.Context, c *module.Context, emit module.Emit) error {
	cfg := discordCfg(c)
	if !cfg.Connected() {
		return nil
	}
	if !h.enabled(cfg) {
		return nil
	}
	var ev T
	if err := codec.Unmarshal(c.Env.Event, &ev); err != nil {
		return err
	}
	emitDiscord(c, cfg.AlertsChannel(), h.line(cfg, ev), emit)
	return nil
}

func raidLine(_ ddiscord.Config, ev discordRaid) string {
	who := chatName(ev.FromBroadcasterUserName, ev.FromBroadcasterUserLogin)
	return who + " is raiding with " + strconv.Itoa(ev.Viewers) + " viewers. Watch on Twitch."
}

func giftLine(cfg ddiscord.Config, ev discordGift) string {
	if ev.Total < atoiDefault(cfg.GiftMin, 5) {
		return ""
	}
	return chatName(ev.UserName, ev.UserLogin) + " gifted " + strconv.Itoa(ev.Total) + " subs."
}

func cheerLine(cfg ddiscord.Config, ev discordCheer) string {
	if ev.Bits < atoiDefault(cfg.CheerMin, 1000) {
		return ""
	}
	return chatName(ev.UserName, ev.UserLogin) + " cheered " + strconv.Itoa(ev.Bits) + " bits."
}

func milestoneLine(_ ddiscord.Config, ev discordSubMsg) string {
	if !isMilestone(ev.CumulativeMonths) {
		return ""
	}
	return chatName(ev.UserName, ev.UserLogin) + " hit " + strconv.Itoa(ev.CumulativeMonths) + " months."
}

// Discord copies raid / gift-bomb / milestone alerts into the connected
// guild. Go-live is NOT here: outgress binds twitch.ingress.event.stream
// directly so live posts never pass through sesame.
func Discord(_ engine.Deps) module.Module {
	m := module.NewModule(discordModuleName, module.KindOptIn)
	m.On("channel.raid", discordCopy[discordRaid]{enabled: ddiscord.Config.RaidOn, line: raidLine}.handle)
	m.On("channel.subscription.gift", discordCopy[discordGift]{enabled: ddiscord.Config.GiftOn, line: giftLine}.handle)
	m.On("channel.cheer", discordCopy[discordCheer]{enabled: ddiscord.Config.CheerOn, line: cheerLine}.handle)
	m.On("channel.subscription.message", discordCopy[discordSubMsg]{enabled: ddiscord.Config.SubMilestoneOn, line: milestoneLine}.handle)
	return m.Build()
}
