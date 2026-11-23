// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package livefeed

import (
	"time"

	"ItsBagelBot/internal/domain/overlays/feed"
	"ItsBagelBot/pkg/valkey"
)

const (
	receiptTTL = 24 * time.Hour
	stateTTL   = 35 * 24 * time.Hour
	controlTTL = 72 * time.Hour
	profileTTL = time.Minute

	defaultSession = "0"
	fieldSession   = "session"
	fieldSeq       = "seq"
)

// channel is a Twitch channel id. Every key of a channel carries its id as the hash tag so one script can touch them all.
type channel string

func (c channel) prefix() string { return "ovf:{" + string(c) + "}:" }

func (c channel) receipt(eventID string) string { return c.prefix() + "r:" + eventID }

func (c channel) counts(period valkey.Period, at time.Time, session string) string {
	return period.Key(c.prefix()+"n", at, session)
}

func (c channel) board(board feed.Board, period valkey.Period, at time.Time, session string) string {
	return period.Key(c.prefix()+"b:"+string(board), at, session)
}

func (c channel) credits(list feed.Kind, session string) string {
	return valkey.PeriodSession.Key(c.prefix()+"c:"+string(list), time.Time{}, session)
}

func (c channel) events(session string) string {
	return valkey.PeriodSession.Key(c.prefix()+"e", time.Time{}, session)
}

func (c channel) latest() string   { return c.prefix() + "latest" }
func (c channel) names() string    { return c.prefix() + "who" }
func (c channel) state() string    { return c.prefix() + "state" }
func (c channel) control() string  { return c.prefix() + "control" }
func (c channel) subathon() string { return c.prefix() + "subathon" }

func profileKey(userID string) string { return "ovf:profile:" + userID }

var creditLists = []feed.Kind{feed.Follow, feed.Sub, feed.GiftSub, feed.Cheer, feed.Raid, feed.Redemption}

var tallyPeriods = []valkey.Period{valkey.PeriodSession, valkey.PeriodWeek, valkey.PeriodMonth}
