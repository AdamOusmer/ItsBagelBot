// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package voiceplan

import (
	"slices"
	"time"
)

type Plan string

const (
	Free       Plan = "free"
	Premium    Plan = "premium"
	Giveaway   Plan = "giveaway"
	AdminGrant Plan = "admin_grant"
	VIP        Plan = "vip"
)

const TokensPerSecond = 25
const DefaultVoice = "Kore"

var monthlyMinutes = map[Plan]int64{
	Free:       0,
	Premium:    60,
	Giveaway:   60,
	AdminGrant: 30,
	VIP:        120,
}

var voices = []string{
	"Zephyr",
	"Puck",
	"Charon",
	"Kore",
	"Fenrir",
	"Leda",
	"Orus",
	"Aoede",
	"Callirrhoe",
	"Autonoe",
	"Enceladus",
	"Iapetus",
	"Umbriel",
	"Algieba",
	"Despina",
	"Erinome",
	"Algenib",
	"Rasalgethi",
	"Laomedeia",
	"Achernar",
	"Alnilam",
	"Schedar",
	"Gacrux",
	"Pulcherrima",
	"Achird",
	"Zubenelgenubi",
	"Vindemiatrix",
	"Sadachbia",
	"Sadaltager",
	"Sulafat",
}

func PlanFor(status, source string, active, banned bool) Plan {
	if !active || banned {
		return Free
	}
	if status == "vip" {
		return VIP
	}
	if status != "paid" {
		return Free
	}
	switch source {
	case "tebex":
		return Premium
	case "giveaway":
		return Giveaway
	case "admin":
		return AdminGrant
	default:
		return Free
	}
}

func MonthlySeconds(plan Plan) int64 {
	return monthlyMinutes[plan] * 60
}

func SecondsToTokens(seconds int64) int64 {
	return seconds * TokensPerSecond
}

func ValidVoice(voice string) bool {
	return slices.Contains(voices, voice)
}

func Period(t time.Time) string {
	return t.UTC().Format("2006-01")
}

func ResetsAt(t time.Time) time.Time {
	year, month, _ := t.UTC().Date()
	return time.Date(year, month+1, 1, 0, 0, 0, 0, time.UTC)
}
