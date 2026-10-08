// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package watchtime_test

import (
	"context"
	"os"
	"strconv"
	"testing"
	"time"

	"ItsBagelBot/internal/domain/event/data"
	"ItsBagelBot/internal/watchtime"
	"ItsBagelBot/pkg/codec"

	"github.com/stretchr/testify/require"
	"github.com/valkey-io/valkey-go"
)

const outboxDB = 15

func realClient(t testing.TB, db int) valkey.Client {
	t.Helper()
	addr := os.Getenv("VALKEY_TEST_ADDR")
	if addr == "" {
		t.Skip("VALKEY_TEST_ADDR requires an isolated real Valkey")
	}
	client, err := valkey.NewClient(valkey.ClientOption{InitAddress: []string{addr}, SelectDB: db, DisableCache: true})
	require.NoError(t, err)
	t.Cleanup(client.Close)
	return client
}

func uniqueID(base uint64) (uint64, string) {
	id := uint64(time.Now().UnixNano()%100000000) + base
	return id, strconv.FormatUint(id, 10)
}

func cleanupKeys(t testing.TB, client valkey.Client, keys ...string) {
	t.Helper()
	t.Cleanup(func() { client.Do(context.Background(), client.B().Del().Key(keys...).Build()) })
}

func outboxClient(t testing.TB) valkey.Client {
	t.Helper()
	client := realClient(t, outboxDB)
	keys := []string{watchtime.Stream, watchtime.QuarantineStream, watchtime.OutboxWindowIndex, watchtime.RetentionFloor, "watchtime:operations:7"}
	require.NoError(t, client.Do(context.Background(), client.B().Del().Key(keys...).Build()).Error())
	cleanupKeys(t, client, keys...)
	return client
}

func testAward() data.WatchAwardDTO {
	return data.WatchAwardDTO{UserID: 7, AccountCreatedAt: 1_700_000_000_000_000, Generation: "2", LiveSession: "session-1", WindowID: "window-1", Entries: []data.LoyaltyEarnEntry{{ViewerID: 8, Points: 10, WatchSeconds: 300}}}
}

func appendAward(t testing.TB, client valkey.Client, award data.WatchAwardDTO, operationID string) string {
	t.Helper()
	body, err := codec.Marshal(award)
	require.NoError(t, err)
	id, err := client.Do(context.Background(), client.B().Xadd().Key(watchtime.Stream).Id("*").FieldValue().FieldValue("operation_id", operationID).FieldValue("payload", string(body)).Build()).ToString()
	require.NoError(t, err)
	return id
}
