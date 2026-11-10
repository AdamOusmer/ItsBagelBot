// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/internal/domain/outgress"
)

const RedemptionAddType = "channel.channel_points_custom_reward_redemption.add"

type RedemptionEvent struct {
	ID                   string `json:"id"`
	BroadcasterUserID    string `json:"broadcaster_user_id"`
	BroadcasterUserLogin string `json:"broadcaster_user_login"`
	UserID               string `json:"user_id"`
	UserName             string `json:"user_name"`
	UserLogin            string `json:"user_login"`
	UserInput            string `json:"user_input"`
	Reward               struct {
		ID    string `json:"id"`
		Title string `json:"title"`
		Cost  int    `json:"cost"`
	} `json:"reward"`
}

func (e RedemptionEvent) Resolve(status string) *module.Output {
	return &module.Output{
		Type:          outgress.TypeRedemptionUpdate,
		BroadcasterID: e.BroadcasterUserID,
		RewardID:      e.Reward.ID,
		RedemptionID:  e.ID,
		Status:        status,
	}
}
