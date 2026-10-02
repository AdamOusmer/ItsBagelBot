// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	usersrpc "ItsBagelBot/internal/domain/rpc/users"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestBasketBudget(t *testing.T) {
	if want := 15 * time.Second; basketBudget != want {
		t.Fatalf("basketBudget = %v, want %v", basketBudget, want)
	}
}

type fakeCoverage struct {
	value usersrpc.PremiumCoverage
	err   error
}

func (f fakeCoverage) Coverage(context.Context, uint64) (usersrpc.PremiumCoverage, error) {
	return f.value, f.err
}

type fakeAwards struct {
	found bool
	err   error
}

func (f fakeAwards) HasPendingOrActiveAward(context.Context, uint64) (bool, error) {
	return f.found, f.err
}

func TestCheckoutGuardAllow(t *testing.T) {
	paidUntil := time.Now().UTC().Add(time.Hour)
	expired := time.Now().UTC().Add(-time.Hour)
	tests := []struct {
		name     string
		coverage fakeCoverage
		awards   fakeAwards
		wantErr  error
	}{
		{"coverage outage", fakeCoverage{err: errors.New("users unavailable")}, fakeAwards{}, errors.New("could not verify premium coverage")},
		{"award read outage", fakeCoverage{}, fakeAwards{err: errors.New("database unavailable")}, errors.New("could not verify premium coverage")},
		{"billing uncertain", fakeCoverage{value: usersrpc.PremiumCoverage{BillingUncertain: true}}, fakeAwards{}, errors.New("could not verify premium coverage")},
		{"active grant", fakeCoverage{value: usersrpc.PremiumCoverage{Grants: []usersrpc.PremiumGrant{{EndAt: paidUntil}}}}, fakeAwards{}, errAlreadyPremium},
		{"expired coverage", fakeCoverage{value: usersrpc.PremiumCoverage{PaidThrough: &expired, Grants: []usersrpc.PremiumGrant{{EndAt: expired}}}}, fakeAwards{}, nil},
		{"paid coverage", fakeCoverage{value: usersrpc.PremiumCoverage{PaidThrough: &paidUntil}}, fakeAwards{}, errAlreadyPremium},
		{"durable award", fakeCoverage{}, fakeAwards{found: true}, errAlreadyPremium},
		{"VIP without paid coverage", fakeCoverage{value: usersrpc.PremiumCoverage{Status: "vip", IsActive: true}}, fakeAwards{}, errAlreadyPremium},
		{"banned without paid coverage", fakeCoverage{value: usersrpc.PremiumCoverage{Banned: true, IsActive: true}}, fakeAwards{}, errAlreadyPremium},
		{"eligible account", fakeCoverage{}, fakeAwards{}, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			guard := &CheckoutGuard{Coverage: tc.coverage, Awards: tc.awards}
			err := guard.Allow(context.Background(), 7)
			switch tc.wantErr {
			case nil:
				require.NoError(t, err)
			case errAlreadyPremium:
				require.ErrorIs(t, err, errAlreadyPremium)
			default:
				require.EqualError(t, err, tc.wantErr.Error())
			}
		})
	}
}
