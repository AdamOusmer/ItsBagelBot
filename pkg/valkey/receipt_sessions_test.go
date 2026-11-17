// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package valkey_test

import (
	"context"
	"testing"
	"time"

	"ItsBagelBot/internal/valkeytest"
	pkgvalkey "ItsBagelBot/pkg/valkey"

	"github.com/stretchr/testify/require"
)

func TestCloseWithReceiptSucceedsWithoutAnEscrowLedger(t *testing.T) {
	ctx := context.Background()
	client := valkeytest.Spawn(t)
	require.NoError(t, client.Do(ctx, client.B().Set().Key("state").Value("open").Build()).Error())
	spec := pkgvalkey.CloseSpec{
		Receipt:      pkgvalkey.Key{Name: "receipt", TTL: time.Hour},
		ReceiptValue: "done",
		Drop:         []string{"state"},
		Escrow:       &pkgvalkey.RenameTTL{From: "entries", To: "snapshot", TTL: time.Hour},
	}

	require.NoError(t, pkgvalkey.CloseWithReceipt(ctx, client, spec))
	receipt, err := client.Do(ctx, client.B().Get().Key("receipt").Build()).ToString()
	require.NoError(t, err)
	live, err := client.Do(ctx, client.B().Exists().Key("state").Build()).AsInt64()
	require.NoError(t, err)
	require.Equal(t, []any{"done", int64(0)}, []any{receipt, live})
}
