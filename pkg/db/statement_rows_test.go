// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package db_test

import (
	"strings"
	"testing"

	"ItsBagelBot/pkg/db"
	"github.com/stretchr/testify/require"
)

func TestStatementRowChunksBudgets(t *testing.T) {
	for _, test := range []struct {
		name  string
		row   []any
		count int
		want  []int
	}{
		{"rows", []any{uint64(1)}, 1001, []int{500, 500, 1}},
		{"bytes", []any{strings.Repeat("é", 32000)}, 5, []int{2, 2, 1}},
		{"wideRows", make([]any, 200), 81, []int{40, 40, 1}},
		{"oversized", []any{strings.Repeat("x", db.StatementBytes)}, 1, nil},
		{"empty", nil, 0, nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			rows := make([][]any, test.count)
			for i := range rows {
				rows[i] = test.row
			}
			chunks, err := db.StatementRowChunks(rows, 256)
			if test.name == "oversized" {
				require.ErrorIs(t, err, db.ErrStatementRowTooLarge)
				return
			}
			require.NoError(t, err)
			var sizes []int
			for _, chunk := range chunks {
				sizes = append(sizes, len(chunk))
			}
			require.Equal(t, test.want, sizes)
		})
	}
}
