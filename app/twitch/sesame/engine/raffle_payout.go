// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"ItsBagelBot/app/twitch/sesame/module"

	"go.uber.org/zap"
)

type RaffleEntrant struct {
	ID    string
	Login string
}

type RaffleWinner struct {
	ID     string `json:"id"`
	Login  string `json:"login"`
	Points int64  `json:"points,omitempty"`
}

type RafflePayout string

const (
	RafflePayoutNone       RafflePayout = ""
	RafflePayoutPaid       RafflePayout = "paid"
	RafflePayoutLoyaltyOff RafflePayout = "loyalty_off"
	RafflePayoutFailed     RafflePayout = "failed"
)

type RaffleWallet interface {
	Credit(ctx context.Context, broadcasterID uint64, winner RaffleWinner) error
}

type loyaltyRaffleWallet struct {
	loyalty LoyaltyStore
}

func NewRaffleWallet(loyalty LoyaltyStore) RaffleWallet {
	if loyalty == nil {
		return nil
	}
	return loyaltyRaffleWallet{loyalty: loyalty}
}

func (w loyaltyRaffleWallet) Credit(ctx context.Context, broadcasterID uint64, winner RaffleWinner) error {
	viewerID, err := strconv.ParseUint(winner.ID, 10, 64)
	if err != nil || viewerID == 0 {
		return fmt.Errorf("raffle: winner %q has no viewer id", winner.Login)
	}
	_, found, err := w.loyalty.BalanceAdjustViewer(ctx, BalanceAdjustment{
		BroadcasterID: broadcasterID, ViewerID: viewerID, ViewerLogin: winner.Login, Value: winner.Points,
	})
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("raffle: loyalty refused credit for %q", winner.Login)
	}
	return nil
}

const raffleCreditAttempts = 3

func splitPrize(prize int64, winners int) []int64 {
	shares := make([]int64, winners)
	if winners == 0 {
		return shares
	}
	base, rem := prize/int64(winners), prize%int64(winners)
	for i := range shares {
		shares[i] = base
		if int64(i) < rem {
			shares[i]++
		}
	}
	return shares
}

func (s *ValkeyRaffleStore) payWinners(ctx context.Context, broadcasterID uint64, res *RaffleResult) {
	if res.Prize <= 0 || len(res.Winners) == 0 {
		return
	}
	if _, on := ReadLoyaltyConfig(ctx, s.cfg.Proj, broadcasterID); !on || s.cfg.Wallet == nil {
		res.Payout = RafflePayoutLoyaltyOff
		return
	}
	res.Payout = RafflePayoutPaid
	for _, w := range res.Winners {
		if err := s.credit(ctx, broadcasterID, w); err != nil {
			res.Payout = RafflePayoutFailed
			s.log.Error("raffle: prize credit failed", module.BIDField(broadcasterID),
				zap.String("viewer_id", w.ID), zap.Int64("points", w.Points), zap.Error(err))
		}
	}
}

func (s *ValkeyRaffleStore) credit(ctx context.Context, broadcasterID uint64, w RaffleWinner) error {
	if w.Points <= 0 {
		return nil
	}
	err := s.cfg.Wallet.Credit(ctx, broadcasterID, w)
	for attempt := 1; err != nil && attempt < raffleCreditAttempts && ctx.Err() == nil; attempt++ {
		time.Sleep(time.Duration(attempt) * 200 * time.Millisecond)
		err = s.cfg.Wallet.Credit(ctx, broadcasterID, w)
	}
	return err
}
