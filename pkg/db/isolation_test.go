// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package db_test

import (
	"database/sql"
	"testing"

	"ItsBagelBot/pkg/db"
	"github.com/stretchr/testify/assert"
)

func TestReadCommittedTx(t *testing.T) {
	for _, tc := range []struct {
		dialect string
		want    *sql.TxOptions
	}{
		{"mysql", &sql.TxOptions{Isolation: sql.LevelReadCommitted}},
		{"sqlite3", nil},
	} {
		t.Run(tc.dialect, func(t *testing.T) {
			assert.Equal(t, tc.want, db.ReadCommittedTx(tc.dialect))
		})
	}
}
