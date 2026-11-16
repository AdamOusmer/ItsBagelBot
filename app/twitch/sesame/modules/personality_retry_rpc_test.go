// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sync/atomic"
	"testing"

	"ItsBagelBot/app/db/modules/ent"
	"ItsBagelBot/app/db/modules/ent/enttest"
	"ItsBagelBot/app/db/modules/repository"
	"ItsBagelBot/app/twitch/sesame/engine"
	domainrpc "ItsBagelBot/internal/domain/rpc"
	modulesrpc "ItsBagelBot/internal/domain/rpc/modules"
	"ItsBagelBot/internal/testdb"
	"ItsBagelBot/internal/testnats"
	"ItsBagelBot/internal/valkeytest"
	"ItsBagelBot/pkg/bus"

	entdialect "entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type retryFeedRPC struct {
	repo  *repository.Personality
	calls atomic.Int32
	ids   chan string
}

func (s *retryFeedRPC) feed(ctx context.Context, req modulesrpc.FeedBumpRequest) modulesrpc.FeedBumpReply {
	s.ids <- req.EventID
	totals, err := s.repo.FeedBump(ctx, req.BroadcasterID, req.Name, req.EventID)
	if err != nil {
		return modulesrpc.FeedBumpReply{Refusal: domainrpc.Refused(domainrpc.CodeInternal, err.Error())}
	}
	if s.calls.Add(1) == 1 {
		return modulesrpc.FeedBumpReply{Refusal: domainrpc.Refused(domainrpc.CodeUnavailable, "response unavailable after commit")}
	}
	return modulesrpc.FeedBumpReply{Total: totals.Total, Channel: totals.Channel, Rank: totals.Rank}
}

func retryFeedClient(t *testing.T, dialect string) *ent.Client {
	t.Helper()
	if dialect == "sqlite" {
		client := testdb.Open(t, testdb.Name(t.Name()), func(driver, dsn string) *ent.Client {
			return enttest.Open(t, driver, dsn)
		})
		require.NoError(t, repository.MigrateSchema(t.Context(), client, entdialect.SQLite))
		return client
	}
	client := ent.NewClient(ent.Driver(entsql.OpenDB("mysql", testdb.MySQL(t, "feed_rpc_retry_"))))
	require.NoError(t, repository.MigrateSchema(t.Context(), client, entdialect.MySQL))
	t.Cleanup(func() { _ = client.Close() })
	return client
}

func TestPersonalityFeedRetriesRPCWithOriginalIdentity(t *testing.T) {
	for _, dialect := range []string{"sqlite", "mysql"} {
		for _, identity := range []string{"message", "chat", "event"} {
			t.Run(dialect+"/"+identity, func(t *testing.T) {
				checkPersonalityFeedRPC(t, retryFeedClient(t, dialect), identity)
			})
		}
	}
}

func checkPersonalityFeedRPC(t *testing.T, client *ent.Client, identity string) {
	t.Helper()
	pinPersonalityRand(t)
	nc := testnats.Connect(t)
	backend := &retryFeedRPC{repo: repository.NewPersonality(client), ids: make(chan string, 3)}
	wiring := bus.RPCWiring{NC: nc, Queue: "feed-test", Log: zap.NewNop()}
	require.NoError(t, bus.Serve(wiring, "test.personality.feed", backend.feed))
	cache := valkeytest.Spawn(t)
	store := engine.NewValkeyPersonality(cache, engine.NewPersonalityRPC(nc, "test"), zap.NewNop())
	m := Personality(engine.Deps{Personality: store, Cooldown: engine.NewValkeyCooldown(cache)})
	c := personalityCtx("feed the bagel")
	c.Env.EventID = "event-one"
	if identity == "message" {
		c.Env.MsgID = "message-one"
	}
	if identity == "chat" {
		c.Env.ChatMessageID = "chat-one"
	}
	var col collector
	require.NoError(t, m.Events[c.Env.Type](t.Context(), c, col.emit))
	total, err := backend.repo.FeedTotal(t.Context())
	require.NoError(t, err)
	channel, _, err := backend.repo.FeedChannel(t.Context(), c.BroadcasterID)
	require.NoError(t, err)
	digest := sha256.Sum256([]byte("2:" + identity + "-one"))
	receiptID := hex.EncodeToString(digest[:])
	require.Equal(t, struct {
		Calls, Receipts, Replies int
		Total, Channel           int64
	}{2, 1, 1, 1, 1}, struct {
		Calls, Receipts, Replies int
		Total, Channel           int64
	}{int(backend.calls.Load()), client.FeedReceipt.Query().CountX(t.Context()), len(col.out), total, channel})
	assert.Equal(t, []string{receiptID, receiptID}, []string{<-backend.ids, <-backend.ids})
	assert.Equal(t, fmt.Sprintf(personalityFeedCountPack[0], 1, 1), col.out[0].Text)
	c.Env.MsgID = "different-event"
	require.NoError(t, m.Events[c.Env.Type](t.Context(), c, col.emit))
	assert.EqualValues(t, 2, backend.calls.Load())
}
