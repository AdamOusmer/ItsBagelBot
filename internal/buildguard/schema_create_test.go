// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package buildguard

import (
	"os"
	"regexp"
	"testing"
)

// db1's default_table_encryption stays ON (table encryption is non-negotiable
// here). A CREATE DATABASE/CREATE SCHEMA issued while that session variable is
// on replicates the variable along with the DDL; the HeatWave replica's
// applier account has no privilege to set it, so the channel stops (errno
// 1227) and has to be unstuck by hand. Schemas are created the safe way
// outside this binary (CREATE under a non-replicated session, then the
// encryption default set locally); no service's own code may run either
// statement. internal/testdb/mysql.go is exempt: it creates and drops
// disposable local test databases, never db1.
var createsSchema = regexp.MustCompile(`(?i)CREATE\s+(DATABASE|SCHEMA)\b`)

const schemaCreateExemptFile = "internal/testdb/mysql.go"

func TestNoServiceCodeCreatesDatabaseSchemas(t *testing.T) {
	for _, path := range goSources(t, "../..") {
		rel := relSlash("../..", path)
		if rel == schemaCreateExemptFile {
			continue
		}
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		if loc := createsSchema.FindIndex(body); loc != nil {
			t.Fatalf("%s runs %q: service code must never create a database/schema on db1;"+
				" a new schema has to come from outside this binary or it will stop the HeatWave replica",
				rel, body[loc[0]:loc[1]])
		}
	}
}
