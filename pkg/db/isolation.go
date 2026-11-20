// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package db

import "database/sql"

func ReadCommittedTx(dialect string) *sql.TxOptions {
	if dialect == "mysql" {
		return &sql.TxOptions{Isolation: sql.LevelReadCommitted}
	}
	return nil
}
