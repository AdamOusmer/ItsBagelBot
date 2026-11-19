// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"ItsBagelBot/app/twitch/sesame/engine/draw"
	"ItsBagelBot/internal/channelruntime"
	"ItsBagelBot/internal/domain/rpc"
	"ItsBagelBot/pkg/bus"
)

type drawPayer struct {
	loyalty  LoyaltyStore
	accounts channelruntime.Reader
}

func NewDrawPayer(loyalty LoyaltyStore, accounts channelruntime.Reader) draw.Payer {
	return drawPayer{loyalty: loyalty, accounts: accounts}
}

func (p drawPayer) Instance(ctx context.Context, channel draw.ChannelID, _ draw.ViewerID) (int64, error) {
	if p.accounts == nil {
		return 0, nil
	}
	account, err := p.accounts.Get(ctx, uint64(channel))
	return account.AccountCreatedAt, err
}

func (p drawPayer) Pay(ctx context.Context, payment draw.Payment) error {
	viewerID, err := strconv.ParseUint(string(payment.Viewer), 10, 64)
	if err != nil {
		return fmt.Errorf("%w: %w", draw.ErrPaymentRejected, err)
	}
	_, err = p.loyalty.BalanceCredit(ctx, PointCredit{
		BroadcasterID: uint64(payment.Channel), ViewerID: viewerID, ViewerLogin: payment.Login,
		Value: payment.Points, Key: string(payment.Key), AccountCreatedAt: payment.Instance,
	})
	if permanentCreditError(err) {
		return fmt.Errorf("%w: %w", draw.ErrPaymentRejected, err)
	}
	return err
}

func permanentCreditError(err error) bool {
	var refusal bus.RPCReplyError
	if !errors.As(err, &refusal) {
		return false
	}
	switch refusal.Code {
	case rpc.CodeInvalid, rpc.CodeForbidden, rpc.CodeConflict:
		return true
	default:
		return false
	}
}
