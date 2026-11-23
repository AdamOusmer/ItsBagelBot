// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAnnouncementsExpandEveryNamespacedToken(t *testing.T) {
	for _, locale := range []string{"en", "fr"} {
		v := duelVoice{locale: locale, points: "bagels"}
		rendered := map[string]string{
			"duel.auto_noshow": duelNoShowText(v, &DuelState{Opener: "alice", Challenged: "bob", OpenerStake: 20}),
			"raffle.remind":    RaffleAnnouncement{Locale: locale}.RemindText(90, 7),
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
