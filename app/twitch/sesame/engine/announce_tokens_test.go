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
			"duel.auto_won":      duelAutoWonText(v, "alice", 40),
			"duel.auto_noshow":   duelNoShowText(v, &DuelState{Opener: "alice", Challenged: "bob", OpenerStake: 20}),
			"raffle.auto_closed": raffleAutoClosedText(locale, &RaffleResult{Winners: []string{"alice", "bob"}, Entrants: 7}),
			"raffle.remind":      raffleRemindText(locale, 90, 7),
		}
		for key, text := range rendered {
			assert.NotContains(t, text, "{", "%s %s left a token: %q", locale, key, text)
			assert.NotContains(t, text, "}", "%s %s left a token: %q", locale, key, text)
		}
		assert.Contains(t, rendered["duel.auto_won"], "@alice")
		assert.Contains(t, rendered["duel.auto_won"], "40 bagels")
		assert.Contains(t, rendered["duel.auto_noshow"], "@bob")
		assert.Contains(t, rendered["raffle.auto_closed"], "@alice, @bob")
		assert.Contains(t, rendered["raffle.remind"], "~2 min")
	}
}
