// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"

	"ItsBagelBot/internal/analytics"
	catalog "ItsBagelBot/internal/domain/analytics"
)

func (p *Pipeline) recordFlag(ctx context.Context, rule flagRule, enforced bool) {
	mode := "shadow"
	if enforced {
		mode = "enforced"
	}
	name := string(flagRuleNames[flagBucket(rule)])
	switch rule {
	case ruleSuffixRepeat:
		name = "repeat"
	case ruleSuffixCampaign:
		name = "campaign"
	}
	p.analytics.AddContext(ctx, catalog.AutomodFlags, 1, name, mode)
}

func entrantBucket(n int64) string {
	switch {
	case n <= 1:
		return "1"
	case n < 5:
		return "2_4"
	case n < 20:
		return "5_19"
	case n < 100:
		return "20_99"
	default:
		return "100p"
	}
}

func recordEntrants(rec *analytics.Recorder, game string, n int64) {
	if n > 0 {
		rec.Count(catalog.GamesEntrants, game, entrantBucket(n))
	}
}

func (s *ValkeyRaffleStore) recordRound(read *drawRead, res *RaffleResult) {
	game := "raffle"
	if read.State.Multi {
		game = "mraffle"
	}
	end := "drawn"
	if res.Entrants == 0 {
		end = "empty"
	}
	s.analytics.Count(catalog.GamesRounds, game, end)
	recordEntrants(s.analytics, game, res.Entrants)
}

func (s *ValkeyDuelStore) recordRound(receipt *DuelReceipt) {
	s.analytics.Count(catalog.GamesRounds, "duel", string(receipt.Outcome))
	recordEntrants(s.analytics, "duel", 2)
}

func (s *ValkeyRaffleStore) SetAnalytics(rec *analytics.Recorder) { s.analytics = rec }
func (s *ValkeyDuelStore) SetAnalytics(rec *analytics.Recorder)   { s.analytics = rec }
func (w *LoyaltyWallet) SetAnalytics(rec *analytics.Recorder)     { w.analytics = rec }
func (t *TrialPromotion) SetAnalytics(rec *analytics.Recorder)    { t.analytics = rec }
