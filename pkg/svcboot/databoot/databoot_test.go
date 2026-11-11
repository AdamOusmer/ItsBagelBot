// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package databoot_test

import (
	"testing"

	"ItsBagelBot/pkg/svcboot"
	"ItsBagelBot/pkg/svcboot/databoot"

	"github.com/stretchr/testify/require"
)

func TestMustEntDriverRequiresConfiguredSchemaBeforeConnecting(t *testing.T) {
	t.Setenv("DB_USER", "test-user")
	t.Setenv("DB_PASS", "test-password")
	t.Setenv("DB_SCHEMA", "")
	t.Setenv("DB_CA_CERT", "")
	require.PanicsWithValue(t, "missing required environment variable: DB_SCHEMA", func() {
		databoot.MustEntDriver(svcboot.Core{})
	})
}
