// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/internal/domain/automation"
	"ItsBagelBot/internal/domain/outgress"
	"ItsBagelBot/pkg/codec"
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

type redemptionSettlement struct {
	Event    RedemptionEvent `json:"event"`
	OnRedeem string          `json:"on_redeem,omitempty"`
}

var redeemOutcomeStatuses = map[string]string{
	"fulfill": outgress.RedemptionFulfilled,
	"cancel":  outgress.RedemptionCanceled,
}

func matchedRedemption(t *automation.CompiledTrigger, raw codec.RawMessage) *redemptionSettlement {
	if t.Plan.Automation.Definition.Trigger.Type != "reward" {
		return nil
	}
	var ev RedemptionEvent
	if err := codec.Unmarshal(raw, &ev); err != nil || ev.ID == "" {
		return nil
	}
	return &redemptionSettlement{Event: ev, OnRedeem: t.OnRedeem}
}

func (run *AutomationRun) settleRedemption(admitted bool, emit module.Emit) {
	redemption := run.redemption
	run.redemption = nil
	if redemption == nil {
		return
	}
	status := outgress.RedemptionCanceled
	if admitted {
		status = redeemOutcomeStatuses[redemption.OnRedeem]
	}
	if status != "" {
		emit(redemption.Event.Resolve(status))
	}
}

func (run *AutomationRun) refundIfRefused(emit module.Emit) {
	if run.Record.Outcome != outcomeDeferred {
		run.settleRedemption(false, emit)
	}
}
