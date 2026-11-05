// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package outgress

const (
	TypeRewardUpdate  = "reward_update"
	TypeRewardRestore = "reward_restore"
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
