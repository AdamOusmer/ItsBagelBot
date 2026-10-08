// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package giveaway

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, errors.New("entropy unavailable") }

func TestDrawPicksDistinctWinnersAndDigestIgnoresInputOrder(t *testing.T) {
	candidates := []Candidate{{UserID: 9}, {UserID: 2}, {UserID: 7}, {UserID: 4}}

	winners, err := DrawWithReader(strings.NewReader("this is deterministic test entropy"), candidates, 3)
	require.NoError(t, err)
	seen := map[uint64]bool{}
	for _, winner := range winners {
		assert.False(t, seen[winner.UserID], "duplicate winner %d", winner.UserID)
		seen[winner.UserID] = true
	}
	assert.Len(t, winners, 3)

	digest, err := PoolDigest(candidates)
	require.NoError(t, err)
	reordered, err := PoolDigest([]Candidate{{UserID: 4}, {UserID: 2}, {UserID: 9}, {UserID: 7}})
	require.NoError(t, err)
	assert.Equal(t, digest, reordered)
	emptyDigest, err := PoolDigest(nil)
	require.NoError(t, err)
	assert.NotEmpty(t, emptyDigest)
}

func TestDrawRefusals(t *testing.T) {
	for _, tc := range []struct {
		name string
		call func() error
		want error
	}{
		{"fails closed when entropy fails", func() error {
			_, err := DrawWithReader(failingReader{}, []Candidate{{UserID: 1}, {UserID: 2}}, 1)
			return err
		}, ErrSecureRandom},
		{"rejects a duplicate user in the pool", func() error {
			_, err := CanonicalCandidates([]Candidate{{UserID: 1}, {UserID: 1}})
			return err
		}, ErrDuplicateUser},
		{"rejects more winners than candidates", func() error {
			_, err := Draw([]Candidate{{UserID: 1}}, 2)
			return err
		}, ErrTooManyWinners},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.ErrorIs(t, tc.call(), tc.want)
		})
	}
}

func TestPrizeInterval(t *testing.T) {
	at := func(year int, month time.Month, day int) time.Time {
		return time.Date(year, month, day, 12, 0, 0, 0, time.UTC)
	}
	for _, tc := range []struct {
		name    string
		start   time.Time
		months  int
		rule    string
		want    time.Time
		wantErr error
	}{
		{name: "adds whole months without wrapping back", start: at(2028, 1, 31), months: 2, rule: "tebex-monthly-v1", want: at(2028, 4, 2)},
		{name: "accepts the product limit", start: at(2026, 9, 15), months: 12, rule: "verified", want: at(2027, 9, 15)},
		{name: "rejects a duration above the product limit", start: at(2026, 9, 15), months: 13, rule: "verified", wantErr: ErrInvalidMonths},
		{name: "TestPromotionalCalendarRuleUsesVersionedCalendarMath", start: at(2026, 9, 15), months: 12, rule: PromotionalCalendarMonthRule, want: at(2027, 9, 15)},
		{name: "TestPromotionalCalendarRuleUsesVersionedCalendarMath leap-year boundary", start: at(2028, 2, 29), months: 12, rule: PromotionalCalendarMonthRule, wantErr: ErrAmbiguousInterval},
		{name: "rejects year zero", start: time.Date(0, 1, 1, 0, 0, 0, 0, time.UTC), months: 1, rule: "verified", wantErr: ErrUnrepresentable},
		{name: "rejects a year past 9999", start: time.Date(9999, 12, 1, 0, 0, 0, 0, time.UTC), months: 1, rule: "verified", wantErr: ErrUnrepresentable},
		{name: "accepts the last representable interval", start: time.Date(9999, 1, 1, 0, 0, 0, 0, time.UTC), months: 11, rule: "verified", want: time.Date(9999, 12, 1, 0, 0, 0, 0, time.UTC)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			end, err := PrizeInterval(tc.start, tc.months, tc.rule)
			require.ErrorIs(t, err, tc.wantErr)
			assert.True(t, tc.want.Equal(end), "end=%s want=%s", end, tc.want)
		})
	}
}

func TestPrizeTotal(t *testing.T) {
	total, err := PrizeTotal(5, 12)
	require.NoError(t, err)
	assert.Equal(t, "60", total)
	_, err = PrizeTotal(5, 13)
	assert.ErrorIs(t, err, ErrInvalidMonths)
}
