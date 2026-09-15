// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Package giveaway contains the Transactions-owned giveaway domain logic.
// Persistence and provider adapters depend on the small interfaces here,
// which keeps random selection and workflow recovery straightforward to test.
package giveaway

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math/big"
	"sort"
	"time"

	"ItsBagelBot/pkg/codec"
)

const DrawAlgorithmVersion = "crypto-rand-partial-fisher-yates-v1"
const MaxPrizeMonths = 12

var (
	ErrInvalidMonths   = errors.New("prize duration must be a positive whole number of months")
	ErrTooManyWinners  = errors.New("winner count exceeds eligible candidate count")
	ErrDuplicateUser   = errors.New("candidate pool contains a duplicate user")
	ErrSecureRandom    = errors.New("secure random source failed")
	ErrMissingPool     = errors.New("candidate pool is empty")
	ErrUnrepresentable = errors.New("prize interval cannot be represented")
)

// Candidate is the minimum immutable identity required for a draw. Eligibility
// facts are resolved by Users before they reach this package.
type Candidate struct {
	UserID          uint64 `json:"user_id"`
	Username        string `json:"username,omitempty"`
	Eligible        bool   `json:"eligible"`
	ExclusionReason string `json:"exclusion_reason,omitempty"`
	Eligibility     any    `json:"eligibility,omitempty"`
}

// CanonicalCandidates sorts by stable account identity and rejects malformed
// pools. Database row order is never used as a source of randomness.
func CanonicalCandidates(in []Candidate) ([]Candidate, error) {
	if len(in) == 0 {
		return nil, ErrMissingPool
	}
	out := append([]Candidate(nil), in...)
	sort.Slice(out, func(i, j int) bool { return out[i].UserID < out[j].UserID })
	for i, candidate := range out {
		if candidate.UserID == 0 {
			return nil, fmt.Errorf("candidate %d has no user id", i)
		}
		if i > 0 && out[i-1].UserID == candidate.UserID {
			return nil, fmt.Errorf("user %d: %w", candidate.UserID, ErrDuplicateUser)
		}
	}
	return out, nil
}

// Draw selects k distinct candidates uniformly without replacement. Int is
// deliberately injected so tests can force source failure; production always
// uses crypto/rand.Reader and never falls back to a PRNG or modulo arithmetic.
func Draw(candidates []Candidate, k int) ([]Candidate, error) {
	return DrawWithReader(rand.Reader, candidates, k)
}

func DrawWithReader(random io.Reader, candidates []Candidate, k int) ([]Candidate, error) {
	pool, err := CanonicalCandidates(candidates)
	if err != nil {
		return nil, err
	}
	if k <= 0 {
		return nil, ErrInvalidWinnerCount(k)
	}
	if k > len(pool) {
		return nil, ErrTooManyWinners
	}
	for i := 0; i < k; i++ {
		remaining := len(pool) - i
		n, err := rand.Int(random, big.NewInt(int64(remaining)))
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrSecureRandom, err)
		}
		j := i + int(n.Int64())
		pool[i], pool[j] = pool[j], pool[i]
	}
	return append([]Candidate(nil), pool[:k]...), nil
}

type invalidWinnerCount int

func (e invalidWinnerCount) Error() string {
	return fmt.Sprintf("winner count must be positive: %d", int(e))
}
func ErrInvalidWinnerCount(k int) error { return invalidWinnerCount(k) }

func PoolDigest(candidates []Candidate) (string, error) {
	pool := candidates
	if len(candidates) > 0 {
		canonical, err := CanonicalCandidates(candidates)
		if err != nil {
			return "", err
		}
		pool = canonical
	}
	// The digest is a commitment to the candidate membership and canonical
	// order. User eligibility evidence is stored separately on each snapshot;
	// changing cosmetic usernames must not invalidate an already-approved pool.
	ids := make([]uint64, len(pool))
	for i, c := range pool {
		ids[i] = c.UserID
	}
	b, err := codec.Marshal(ids)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

// PrizeInterval adds calendar months one at a time. It refuses overflow and
// never clamps or silently shortens an interval. rule is persisted beside the
// award so a later policy change cannot alter an existing prize.
func PrizeInterval(start time.Time, months int, rule string) (time.Time, error) {
	if !validIntervalRequest(start, months, rule) {
		return time.Time{}, ErrInvalidMonths
	}
	if !representableInterval(start, months) {
		return time.Time{}, ErrUnrepresentable
	}
	return addCalendarMonths(start, months)
}

func validIntervalRequest(start time.Time, months int, rule string) bool {
	return ValidateMonths(months) == nil && rule != ""
}

func representableInterval(start time.Time, months int) bool {
	if start.Year() < 1 || start.Year() > 9999 {
		return false
	}
	maxMonths := (9999-start.Year())*12 + (12 - int(start.Month()))
	return maxMonths >= 0 && months <= maxMonths
}

func addCalendarMonths(start time.Time, months int) (time.Time, error) {
	end := start
	for i := 0; i < months; i++ {
		next := end.AddDate(0, 1, 0)
		if !next.After(end) || next.Year() > 9999 {
			return time.Time{}, ErrUnrepresentable
		}
		end = next
	}
	return end, nil
}

func ValidateMonths(months int) error {
	if months <= 0 || months > MaxPrizeMonths {
		return ErrInvalidMonths
	}
	return nil
}

// PrizeTotal is overflow-safe and useful for admin summaries. The decimal
// string avoids wrapping a machine integer when an uncapped request is large.
func PrizeTotal(winners, months int) (string, error) {
	if winners <= 0 || ValidateMonths(months) != nil {
		return "", ErrInvalidMonths
	}
	return new(big.Int).Mul(big.NewInt(int64(winners)), big.NewInt(int64(months))).String(), nil
}
