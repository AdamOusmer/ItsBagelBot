// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"encoding/json"
	"math"
	"strconv"
	"testing"

	"ItsBagelBot/app/db/loyalty/ent"
	"github.com/stretchr/testify/require"
)

func TestBalanceReplyPreservesExactPointDigits(t *testing.T) {
	for _, points := range []int64{9007199254740993, math.MaxInt64, math.MinInt64} {
		t.Run(strconv.FormatInt(points, 10), func(t *testing.T) {
			body, err := json.Marshal(balanceView(&ent.Balance{ViewerID: 8, Points: points}))
			require.NoError(t, err)
			var wire map[string]json.RawMessage
			require.NoError(t, json.Unmarshal(body, &wire))
			require.Equal(t, strconv.FormatInt(points, 10), string(wire["points"]), "legacy field stays numeric")
			require.Equal(t, `"`+strconv.FormatInt(points, 10)+`"`, string(wire["points_exact"]), "browser field stays decimal text")
		})
	}
}
