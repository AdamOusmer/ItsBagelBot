// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"context"

	"ItsBagelBot/app/discord/engine/internal/cmd"
	"ItsBagelBot/app/discord/engine/internal/decode"
	"ItsBagelBot/app/discord/engine/module"
	"ItsBagelBot/internal/discordapi"
	"ItsBagelBot/internal/discordstore"
	ddiscord "ItsBagelBot/internal/domain/discord"
)

// Rank ports app/dingress/internal/community/slash.go's daily/rank commands
// and the daily-claim button. Crumbs XP itself is awarded by Message; this
// module only reads and spends it.
func Rank(store discordstore.Store) module.Module {
	h := rankModule{store: store}
	b := module.NewModule("rank")
	b.Slash("daily", h.daily)
	b.Slash("rank", h.rank)
	b.Button(discordapi.CustomDailyClaim, h.daily)
	return b.Build()
}

type rankModule struct {
	store discordstore.Store
}

func (h rankModule) daily(ctx context.Context, c *module.Context, emit module.Emit) error {
	in, err := decode.Decode[decode.InteractionEvent](c.Event.Raw)
	if err != nil {
		return err
	}
	if !c.Config.LevelsOn() {
		emit(cmd.Followup(c.Config.GuildID, in.Token, "Levels are off.", true))
		return nil
	}
	ok, xp := h.store.ClaimDaily(ctx, discordstore.Member{GuildID: in.GuildID, UserID: in.Member.User.ID})
	emit(cmd.FollowupEmbed(c.Config.GuildID, in.Token, ddiscord.DailyEmbed(ddiscord.DailyCard{XP: xp, Fresh: ok}), nil))
	return nil
}

func (h rankModule) rank(ctx context.Context, c *module.Context, emit module.Emit) error {
	in, err := decode.Decode[decode.InteractionEvent](c.Event.Raw)
	if err != nil {
		return err
	}
	if !c.Config.LevelsOn() {
		emit(cmd.Followup(c.Config.GuildID, in.Token, "Levels are off.", true))
		return nil
	}
	userID := decode.OptionUser(in.Data.Options, "user")
	if userID == "" {
		userID = in.Member.User.ID
	}
	xp, level := h.store.Rank(ctx, discordstore.Member{GuildID: in.GuildID, UserID: userID})
	card := ddiscord.RankEmbed(ddiscord.RankCard{Who: decode.Mention(decode.UserRef{ID: userID}), Level: level, XP: xp})

	var buttons []ddiscord.ButtonSpec
	if userID == in.Member.User.ID {
		buttons = []ddiscord.ButtonSpec{{Style: discordapi.ButtonPrimary, Label: "Claim daily", CustomID: discordapi.CustomDailyClaim}}
	}
	emit(cmd.FollowupEmbed(c.Config.GuildID, in.Token, card, buttons))
	return nil
}
