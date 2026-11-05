// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package outgress

import "time"

const (
	TypeRewardUpdate  = "reward_update"
	TypeRewardRestore = "reward_restore"

	RewardResultOK            = "ok"
	RewardResultNotManageable = "reward_not_manageable"
	RewardResultNotFound      = "reward_not_found"
	RewardResultRejected      = "reward_rejected"
	RewardResultUnconfirmed   = "reward_unconfirmed"

	RewardResultTTL = 7 * 24 * time.Hour
)

type RewardUpdate struct {
	Enabled          *bool `json:"enabled,omitempty"`
	Cost             *int  `json:"cost,omitempty"`
	ResetOnStreamEnd bool  `json:"reset_on_stream_end,omitempty"`
}

// Outgress writes this hash; sesame only checks that it exists to decide whether a stream end owes a restore.
func RewardSnapshotKey(broadcasterID string) string {
	return "outgress:rewards:snapshot:" + broadcasterID
}

// Outgress writes this hash, action slot to result; sesame overlays it on the run log.
func RewardResultKey(broadcasterID, runID string) string {
	return "outgress:rewards:result:{" + broadcasterID + "}:" + runID
}
