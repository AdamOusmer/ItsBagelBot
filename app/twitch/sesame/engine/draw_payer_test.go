// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine_test

import (
	"context"
	"errors"
	"testing"

	"ItsBagelBot/app/twitch/sesame/engine"
	"ItsBagelBot/app/twitch/sesame/engine/draw"
	"ItsBagelBot/internal/channelruntime"
	"ItsBagelBot/internal/domain/rpc"
	"ItsBagelBot/pkg/bus"

	"github.com/stretchr/testify/require"
)

type creditWallet struct {
	engine.LoyaltyStore
	credit engine.PointCredit
	err    error
}

func (w *creditWallet) BalanceCredit(_ context.Context, credit engine.PointCredit) (engine.CreditOutcome, error) {
	w.credit = credit
	return engine.CreditOutcome{}, w.err
}

type payerAccount struct {
	channelruntime.Snapshot
	err error
}

func (a payerAccount) Get(context.Context, uint64) (channelruntime.Snapshot, error) {
	return a.Snapshot, a.err
}

func TestDrawPayerMapsCreditRefusalsAndCarriesInstance(t *testing.T) {
	cases := []struct {
		name      string
		err       error
		permanent bool
	}{
		{name: "success"},
		{name: "invalid or overflow", err: bus.RPCReplyError{Code: rpc.CodeInvalid}, permanent: true},
		{name: "retired or replaced account", err: bus.RPCReplyError{Code: rpc.CodeForbidden}, permanent: true},
		{name: "dedup payload conflict", err: bus.RPCReplyError{Code: rpc.CodeConflict}, permanent: true},
		{name: "timeout", err: context.DeadlineExceeded},
		{name: "unavailable", err: bus.RPCReplyError{Code: rpc.CodeUnavailable}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			wallet := &creditWallet{err: tc.err}
			p := engine.NewDrawPayer(wallet, payerAccount{Snapshot: channelruntime.Snapshot{AccountCreatedAt: 301, Registered: true}})
			instance, err := p.Instance(t.Context(), 8, "17")
			require.NoError(t, err)
			err = p.Pay(t.Context(), draw.Payment{Channel: 8, Viewer: "17", Login: "viewer",
				Points: 65, Key: "raffle:a:2", Instance: instance})
			require.Equal(t, tc.permanent, errors.Is(err, draw.ErrPaymentRejected))
			require.ErrorIs(t, err, tc.err)
			require.Equal(t, engine.PointCredit{BroadcasterID: 8, ViewerID: 17, ViewerLogin: "viewer",
				Value: 65, Key: "raffle:a:2", AccountCreatedAt: 301}, wallet.credit)
		})
	}
}

func TestDrawPayerRequiresRegisteredAccountInstance(t *testing.T) {
	cases := []struct {
		name     string
		accounts payerAccount
	}{
		{"unregistered", payerAccount{Snapshot: channelruntime.Snapshot{AccountCreatedAt: 301}}},
		{"zero instance", payerAccount{Snapshot: channelruntime.Snapshot{Registered: true}}},
		{"lookup unavailable", payerAccount{err: context.DeadlineExceeded}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := engine.NewDrawPayer(&creditWallet{}, tc.accounts)
			instance, err := p.Instance(t.Context(), 8, "17")
			require.Error(t, err)
			require.NotErrorIs(t, err, draw.ErrPaymentRejected)
			require.Zero(t, instance)
		})
	}
}
