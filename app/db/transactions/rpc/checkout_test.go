// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	usersrpc "ItsBagelBot/internal/domain/rpc/users"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestSanitizeGiftMessage(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"trims", "  hi there  ", "hi there"},
		{"keeps newlines", "line1\nline2", "line1\nline2"},
		{"tabs become spaces", "a\tb", "a b"},
		{"strips control chars", "hi\x00\x07 there", "hi there"},
		{"empty stays empty", "   ", ""},
		{"caps Unicode by runes", strings.Repeat("é", 400), strings.Repeat("é", giftMessageMaxRunes)},
	}
	for _, tc := range tests {
		if got := sanitizeGiftMessage(tc.in); got != tc.want {
			t.Errorf("%s: sanitizeGiftMessage(%q) = %q, want %q", tc.name, tc.in, got, tc.want)
		}
	}
}

func TestClampLogin(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"short passes", "bagelfan", "bagelfan"},
		{"trims", "  bagelfan  ", "bagelfan"},
		{"at max", strings.Repeat("a", twitchLoginMaxLen), strings.Repeat("a", twitchLoginMaxLen)},
		{"over max truncates", strings.Repeat("a", 100), strings.Repeat("a", twitchLoginMaxLen)},
	}
	for _, tc := range tests {
		if got := clampLogin(tc.in); got != tc.want {
			t.Errorf("%s: clampLogin(%q) = %q, want %q", tc.name, tc.in, got, tc.want)
		}
	}
}

func TestGiftNoteLinkAfterSanitize(t *testing.T) {
	cases := []struct {
		note    string
		blocked bool
	}{
		{"visit example.com now", true},
		{"go to example . com", true},
		{"hey\x00example[.]com", true},
		{"ping me user (at) gmail dot com", true},
		{"thanks so much, enjoy premium!", false},
		{"see you at 3 p.m.", false},
	}
	for _, tc := range cases {
		if noteHasLink(sanitizeGiftMessage(tc.note)) != tc.blocked {
			t.Errorf("gift note %q link detection mismatch", tc.note)
		}
	}
}

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
	tests := []struct {
		name     string
		coverage fakeCoverage
		awards   fakeAwards
		wantErr  error
	}{
		{"coverage outage", fakeCoverage{err: errors.New("users unavailable")}, fakeAwards{}, errors.New("could not verify premium coverage")},
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
