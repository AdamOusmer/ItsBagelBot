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
	"unicode/utf8"
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
	}
	for _, tc := range tests {
		if got := sanitizeGiftMessage(tc.in); got != tc.want {
			t.Errorf("%s: sanitizeGiftMessage(%q) = %q, want %q", tc.name, tc.in, got, tc.want)
		}
	}
}

func TestSanitizeGiftMessageCaps(t *testing.T) {
	long := strings.Repeat("é", 400) // multi-byte runes to prove the cap counts runes, not bytes
	got := sanitizeGiftMessage(long)
	if n := utf8.RuneCountInString(got); n > giftMessageMaxRunes {
		t.Errorf("capped length = %d runes, want <= %d", n, giftMessageMaxRunes)
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

// The checkout gate rejects a gift note only after sanitizing, so a link
// smuggled through control chars or spacing must survive sanitization and still
// be caught. This proves the sanitize -> ContainsLink pairing the RPC relies on.
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

// TestBasketBudget pins the widest handler budget in the service. It was a
// positional argument to a seven-argument subscribe call, which is exactly the
// kind of value a refactor flattens onto the 2s default: two upstream Tebex
// calls do not fit in two seconds, and the failure would be a timeout in
// production rather than a compile error here.
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

func TestCheckoutGuardFailsClosedAndBlocksCoverage(t *testing.T) {
	guard := &CheckoutGuard{Coverage: fakeCoverage{err: errors.New("users unavailable")}, Awards: fakeAwards{}}
	if err := guard.Allow(context.Background(), 7); err == nil {
		t.Fatal("coverage outage was allowed")
	}
	paidUntil := time.Now().UTC().Add(time.Hour)
	guard = &CheckoutGuard{Coverage: fakeCoverage{value: usersrpc.PremiumCoverage{PaidThrough: &paidUntil}}, Awards: fakeAwards{}}
	if err := guard.Allow(context.Background(), 7); err == nil {
		t.Fatal("paid coverage was allowed")
	}
	guard = &CheckoutGuard{Coverage: fakeCoverage{}, Awards: fakeAwards{found: true}}
	if err := guard.Allow(context.Background(), 7); err == nil {
		t.Fatal("durable award was allowed")
	}
}

func TestCheckoutGuardBlocksVipAndBannedAccountsWithoutPaidThrough(t *testing.T) {
	for _, coverage := range []usersrpc.PremiumCoverage{
		{Status: "vip", IsActive: true},
		{Banned: true, IsActive: true},
	} {
		guard := &CheckoutGuard{Coverage: fakeCoverage{value: coverage}, Awards: fakeAwards{}}
		if err := guard.Allow(context.Background(), 7); err == nil {
			t.Fatalf("premium checkout was allowed for coverage=%+v", coverage)
		}
	}
}
