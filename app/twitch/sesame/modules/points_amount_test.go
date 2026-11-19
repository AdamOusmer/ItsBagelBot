// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParsePointsAmount(t *testing.T) {
	cases := []struct {
		raw     string
		balance int64
		want    int64
		valid   bool
	}{
		{"100", 0, 100, true},
		{"+100", 0, 100, true},
		{"0", 0, 0, true},
		{"9223372036854775807", 0, math.MaxInt64, true},
		{"9223372036854775808", 0, math.MaxInt64, true},
		{"5k", 0, 5000, true},
		{" 1.5K ", 0, 1500, true},
		{".5m", 0, 500000, true},
		{"2.M", 0, 2000000, true},
		{"1.0005k", 0, 1001, true},
		{"1.0004k", 0, 1000, true},
		{"9007199254740.993k", 0, 9007199254740993, true},
		{"9223372036854775.806k", 0, math.MaxInt64 - 1, true},
		{"9223372036854.775808m", 0, math.MaxInt64, true},
		{"Half", 1235, 617, true},
		{"ALL", 1235, 1235, true},
		{"all", 0, 0, true},
		{"", 0, 0, false},
		{"lots", 0, 0, false},
		{"k", 0, 0, false},
		{".m", 0, 0, false},
		{"1.5", 0, 0, false},
		{"1.5.5m", 0, 0, false},
		{"1e3k", 0, 0, false},
		{"1_000", 0, 0, false},
		{"1 000k", 0, 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.raw, func(t *testing.T) {
			amount := ParsePointsAmount(tc.raw)
			assert.Equal(t, tc.valid, amount.Valid())
			assert.Equal(t, tc.want, amount.Resolve(tc.balance))
		})
	}
}
