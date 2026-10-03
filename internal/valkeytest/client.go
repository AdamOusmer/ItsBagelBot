// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package valkeytest

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/valkey-io/valkey-go"
)

// Real connects to VALKEY_TEST_ADDR and skips the test when it is unset.
func Real(t testing.TB) valkey.Client {
	t.Helper()
	addr := os.Getenv("VALKEY_TEST_ADDR")
	if addr == "" {
		t.Skip("VALKEY_TEST_ADDR is not set")
	}
	client, err := valkey.NewClient(valkey.ClientOption{
		InitAddress: []string{addr},
		Password:    os.Getenv("VALKEY_TEST_PASSWORD"),
	})
	require.NoError(t, err)
	t.Cleanup(client.Close)
	return client
}

// Client is the real Valkey when VALKEY_TEST_ADDR is set, else an in-process Server.
func Client(t testing.TB) valkey.Client {
	t.Helper()
	if os.Getenv("VALKEY_TEST_ADDR") != "" {
		return Real(t)
	}
	return New(t).Client()
}
