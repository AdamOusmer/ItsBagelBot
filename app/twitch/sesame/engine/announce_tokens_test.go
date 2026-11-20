// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAnnouncementsExpandEveryNamespacedToken(t *testing.T) {
	for _, locale := range []string{"en", "fr"} {
		v := duelVoice{locale: locale, points: "bagels"}
		rendered := map[string]string{
			"duel.auto_noshow": duelNoShowText(v, &DuelState{Opener: "alice", Challenged: "bob", OpenerStake: 20}),
			"raffle.remind":    RaffleAnnouncement{Locale: locale}.RemindText(90, 7),
			"raffle.remind.cl": RaffleAnnouncement{Locale: locale, Config: LegacyRaffleConfig{ClaimEnabled: "on"}}.RemindText(90, 7),
		}
		for key, text := range rendered {
			assert.NotContains(t, text, "{", "%s %s left a token: %q", locale, key, text)
			assert.NotContains(t, text, "}", "%s %s left a token: %q", locale, key, text)
		}
		assert.Contains(t, rendered["duel.auto_noshow"], "@bob")
		assert.Contains(t, rendered["raffle.remind"], "~2 min")
	}
}

func TestDuelAutoWinBalance(t *testing.T) {
	cases := []struct {
		locale string
		suffix string
	}{
		{"en", " Balance: 140 bagels."},
		{"fr", " Solde : 140 bagels."},
		{"de", " Kontostand: 140 bagels."},
		{"es", " Saldo: 140 bagels."},
		{"pt-br", " Saldo: 140 bagels."},
		{"ru", " Баланс: 140 bagels."},
	}
	for _, tc := range cases {
		t.Run(tc.locale, func(t *testing.T) {
			voice := duelVoice{locale: tc.locale, points: "bagels"}
			payout := DuelStake{Login: "alice", Stake: 40}
			withoutBalance := duelAutoWonText(voice, payout, "")
			assert.Regexp(t, `@alice[^{}]*40 bagels[ !.]*$`, withoutBalance)
			assert.Equal(t, withoutBalance+tc.suffix, duelAutoWonText(voice, payout, "140"))
		})
	}
}

func TestRaffleResultLinesExpandEveryTokenInEveryLocale(t *testing.T) {
	winners := []RaffleWinner{{ID: "1", Login: "alice", Points: 500}, {ID: "2", Login: "bob", Points: 500}}
	results := map[string]*RaffleResult{
		"paid":        {Winners: winners, Entrants: 7, Prize: 1000, Payout: RafflePayoutPaid},
		"loyalty off": {Winners: winners, Entrants: 7, Prize: 1000, Payout: RafflePayoutLoyaltyOff},
		"failed":      {Winners: winners, Entrants: 7, Prize: 1000, Payout: RafflePayoutFailed},
		"no prize":    {Winners: winners, Entrants: 7},
		"empty":       {Entrants: 0},
	}
	for _, locale := range []string{"en", "fr", "de", "es", "pt-br", "ru"} {
		for _, claim := range []string{"", "on"} {
			a := RaffleAnnouncement{Locale: locale, Points: "bagels", Config: LegacyRaffleConfig{ClaimEnabled: claim}}
			for name, res := range results {
				for _, line := range a.ResultLines(res) {
					assert.NotContains(t, line, "{", "%s claim=%q %s left a token: %q", locale, claim, name, line)
					assert.NotContains(t, line, "}", "%s claim=%q %s left a token: %q", locale, claim, name, line)
				}
			}
			assert.Contains(t, a.ResultLines(results["paid"])[0], "@alice, @bob", locale)
			assert.Contains(t, a.ResultLines(results["paid"])[0], "500 bagels", locale)
			assert.Len(t, a.ResultLines(results["loyalty off"]), 1, locale)
			assert.NotContains(t, a.ResultLines(results["loyalty off"])[0], "500", locale)
			assert.Equal(t, claim == "on", strings.Contains(a.ResultLines(results["paid"])[0], "!claim"), "%s claim=%q", locale, claim)
			assert.Equal(t, claim == "on", strings.Contains(a.ResultLines(results["no prize"])[0], "!claim"), "%s claim=%q", locale, claim)
		}
		off := RaffleAnnouncement{Locale: locale, Points: "bagels", Config: LegacyRaffleConfig{RaffleModuleConfig: RaffleModuleConfig{WonEnabled: "off"}}}
		assert.Empty(t, off.ResultLines(results["loyalty off"]), locale)
		assert.Len(t, off.ResultLines(results["failed"]), 1, locale)
		assert.Empty(t, off.ResultLines(results["paid"]), locale)
		custom := RaffleAnnouncement{Locale: locale, Config: LegacyRaffleConfig{ClaimEnabled: "on", ClaimPrize: "a signed poster"}}
		assert.Contains(t, custom.ResultLines(results["no prize"])[0], "a signed poster", locale)
	}
}
