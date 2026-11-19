// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package automation

import "regexp"

const (
	maxRewardIDLength = 64
	maxRewardCost     = 10000000
)

var rewardIDPattern = regexp.MustCompile(`^[A-Za-z0-9-]+$`)

type RewardUpdateAction struct {
	RewardID         string `json:"reward_id"`
	Enabled          *bool  `json:"enabled"`
	Cost             *int   `json:"cost"`
	ResetOnStreamEnd bool   `json:"reset_on_stream_end"`
	Paused           *bool  `json:"paused,omitempty"`
	CooldownS        *int   `json:"cooldown_s,omitempty"`
}

func checkRewardUpdate(c chk, p RewardUpdateAction) {
	if c.text("reward_id", p.RewardID, nonEmpty(maxRewardIDLength)) && !rewardIDPattern.MatchString(p.RewardID) {
		c.add("reward_id", CodeInvalidFormat)
	}
	if !p.hasChange() {
		c.add("enabled", CodeRequired)
	}
	if p.Cost != nil {
		inRange(c, "cost", *p.Cost, between(1, maxRewardCost))
	}
	if p.CooldownS != nil {
		inRange(c, "cooldown_s", *p.CooldownS, between(0, MaxRewardCooldown))
	}
}

func (p RewardUpdateAction) hasChange() bool {
	changesState := p.Enabled != nil || p.Paused != nil
	changesAmount := p.Cost != nil || p.CooldownS != nil
	return changesState || changesAmount
}
