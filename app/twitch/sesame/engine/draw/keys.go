// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package draw

import (
	"ItsBagelBot/pkg/cache"
	"ItsBagelBot/pkg/valkey"
)

type ChannelID uint64
type RoundID string

const (
	LegacyRaffleStatePrefix    = "raffle:state:"
	LegacyRaffleDeadlinePrefix = "raffle:deadline:"
	dueKey                     = "draw:due"
	wakeChannel                = "draw:due:wake"
)

func JoinableKey(channel ChannelID) string { return cache.UserKey("draw:joinable:", uint64(channel)) }
func activeKey(channel ChannelID) string   { return cache.UserKey("draw:active:", uint64(channel)) }
func claimableKey(channel ChannelID) string {
	return cache.UserKey("draw:claimable:", uint64(channel))
}

func roundKey(id RoundID) string   { return "draw:round:" + string(id) }
func entriesKey(id RoundID) string { return "draw:entries:" + string(id) }
func labelsKey(id RoundID) string  { return "draw:labels:" + string(id) }
func slotsKey(id RoundID) string   { return "draw:slots:" + string(id) }
func leaseKey(id RoundID) string   { return "draw:lease:" + string(id) }

func keysFor(channel ChannelID, id RoundID) valkey.RoundKeys {
	return valkey.RoundKeys{
		Joinable: JoinableKey(channel), Active: activeKey(channel), Claimable: claimableKey(channel),
		Round: roundKey(id), Entries: entriesKey(id), Labels: labelsKey(id), Slots: slotsKey(id), Lease: leaseKey(id),
		Due: dueKey, Refused: "draw:refused:" + string(id),
		RefusalSeen: "draw:refusal-seen:" + string(id), RefusalPending: "draw:refusal-pending:" + string(id),
	}
}

func legacyStateKey(channel ChannelID) string {
	return cache.UserKey(LegacyRaffleStatePrefix, uint64(channel))
}

func legacyDeadlineKey(channel ChannelID) string {
	return cache.UserKey(LegacyRaffleDeadlinePrefix, uint64(channel))
}
