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
const CharsPerSecond = 14
const MaxSpeechRunes = 500
const DefaultVoice = "Kore"
const MinClipSeconds = 5
const MaxClipSeconds = 120

var monthlyMinutes = map[Plan]int64{
	Free:       0,
	Premium:    60,
	Giveaway:   60,
	AdminGrant: 30,
	VIP:        120,
}

type Voice struct {
	Name      string
	Character string
}

var voices = []Voice{
	{"Zephyr", "Bright"},
	{"Puck", "Upbeat"},
	{"Charon", "Informative"},
	{"Kore", "Firm"},
	{"Fenrir", "Excitable"},
	{"Leda", "Youthful"},
	{"Orus", "Firm"},
	{"Aoede", "Breezy"},
	{"Callirrhoe", "Easy-going"},
	{"Autonoe", "Bright"},
	{"Enceladus", "Breathy"},
	{"Iapetus", "Clear"},
	{"Umbriel", "Easy-going"},
	{"Algieba", "Smooth"},
	{"Despina", "Smooth"},
	{"Erinome", "Clear"},
	{"Algenib", "Gravelly"},
	{"Rasalgethi", "Informative"},
	{"Laomedeia", "Upbeat"},
	{"Achernar", "Soft"},
	{"Alnilam", "Firm"},
	{"Schedar", "Even"},
	{"Gacrux", "Mature"},
	{"Pulcherrima", "Forward"},
	{"Achird", "Friendly"},
	{"Zubenelgenubi", "Casual"},
	{"Vindemiatrix", "Gentle"},
	{"Sadachbia", "Lively"},
	{"Sadaltager", "Knowledgeable"},
	{"Sulafat", "Warm"},
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
	return slices.ContainsFunc(voices, func(entry Voice) bool { return entry.Name == voice })
}

func VoiceNames() []string {
	names := make([]string, 0, len(voices))
	for _, voice := range voices {
		names = append(names, voice.Name)
	}
	return names
}

func Period(t time.Time) string {
	return t.UTC().Format("2006-01")
}

func ResetsAt(t time.Time) time.Time {
	year, month, _ := t.UTC().Date()
	return time.Date(year, month+1, 1, 0, 0, 0, 0, time.UTC)
}
