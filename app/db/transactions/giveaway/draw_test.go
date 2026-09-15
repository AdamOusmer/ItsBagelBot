package giveaway

import (
	"errors"
	"strings"
	"testing"
	"time"
)

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, errors.New("entropy unavailable") }

func TestDrawCanonicalDistinctAndDigest(t *testing.T) {
	candidates := []Candidate{{UserID: 9}, {UserID: 2}, {UserID: 7}, {UserID: 4}}
	got, err := DrawWithReader(strings.NewReader("this is deterministic test entropy"), candidates, 3)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[uint64]bool{}
	for _, c := range got {
		if seen[c.UserID] {
			t.Fatalf("duplicate winner %d", c.UserID)
		}
		seen[c.UserID] = true
	}
	if len(got) != 3 {
		t.Fatalf("winners=%d", len(got))
	}
	digest, err := PoolDigest(candidates)
	if err != nil {
		t.Fatal(err)
	}
	digest2, err := PoolDigest([]Candidate{{UserID: 4}, {UserID: 2}, {UserID: 9}, {UserID: 7}})
	if err != nil {
		t.Fatal(err)
	}
	if digest != digest2 {
		t.Fatalf("digest depends on input order: %s != %s", digest, digest2)
	}
}

func TestDrawFailsClosedWhenEntropyFails(t *testing.T) {
	_, err := DrawWithReader(failingReader{}, []Candidate{{UserID: 1}, {UserID: 2}}, 1)
	if !errors.Is(err, ErrSecureRandom) {
		t.Fatalf("err=%v", err)
	}
}

func TestDurationNoProductCapAndNoWrap(t *testing.T) {
	if err := ValidateMonths(12); err != nil {
		t.Fatal(err)
	}
	if err := ValidateMonths(13); err == nil {
		t.Fatal("duration above the product limit accepted")
	}
	start := time.Date(2028, 1, 31, 12, 0, 0, 0, time.UTC)
	end, err := PrizeInterval(start, 2, "tebex-monthly-v1")
	if err != nil {
		t.Fatal(err)
	}
	if !end.After(start) {
		t.Fatalf("end=%s", end)
	}
	if total, err := PrizeTotal(5, 12); err != nil || total != "60" {
		t.Fatalf("total=%q err=%v", total, err)
	}
}

func TestPromotionalCalendarRuleUsesVersionedCalendarMath(t *testing.T) {
	start := time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC)
	end, err := PrizeInterval(start, 12, PromotionalCalendarMonthRule)
	if err != nil {
		t.Fatal(err)
	}
	want := time.Date(2027, 9, 15, 12, 0, 0, 0, time.UTC)
	if !end.Equal(want) {
		t.Fatalf("end=%s want=%s", end, want)
	}
	_, err = PrizeInterval(time.Date(2028, 2, 29, 12, 0, 0, 0, time.UTC), 12, PromotionalCalendarMonthRule)
	if !errors.Is(err, ErrAmbiguousInterval) {
		t.Fatalf("leap-year boundary err=%v", err)
	}
}

func TestDurationRejectsUnrepresentableYears(t *testing.T) {
	if _, err := PrizeInterval(time.Date(0, 1, 1, 0, 0, 0, 0, time.UTC), 1, "verified"); !errors.Is(err, ErrUnrepresentable) {
		t.Fatalf("year zero err=%v", err)
	}
	if _, err := PrizeInterval(time.Date(9999, 12, 1, 0, 0, 0, 0, time.UTC), 1, "verified"); !errors.Is(err, ErrUnrepresentable) {
		t.Fatalf("year overflow err=%v", err)
	}
	if _, err := PrizeInterval(time.Date(9999, 1, 1, 0, 0, 0, 0, time.UTC), 11, "verified"); err != nil {
		t.Fatalf("last representable interval err=%v", err)
	}
}

func TestInvalidPoolAndWinnerCount(t *testing.T) {
	if digest, err := PoolDigest(nil); err != nil || digest == "" {
		t.Fatalf("empty pool digest=%q err=%v", digest, err)
	}
	if _, err := CanonicalCandidates([]Candidate{{UserID: 1}, {UserID: 1}}); !errors.Is(err, ErrDuplicateUser) {
		t.Fatalf("duplicate err=%v", err)
	}
	if _, err := Draw([]Candidate{{UserID: 1}}, 2); !errors.Is(err, ErrTooManyWinners) {
		t.Fatalf("count err=%v", err)
	}
}
