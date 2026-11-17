// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package worker

import (
	"reflect"
	"testing"
	"time"

	"ItsBagelBot/internal/domain/invalidate"
	usersrpc "ItsBagelBot/internal/domain/rpc/users"
	"ItsBagelBot/internal/testnats"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/cache"
	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func seededUserIDs(t *testing.T, feed cache.Feed) *cache.Cache[string] {
	t.Helper()
	c := NewUserIDCache(feed)
	t.Cleanup(c.Close)
	c.Set("login:oldname", "42")
	c.Set("login:newname", "")
	c.Set("login:other", "84")
	return c
}

func cachedLogins(c *cache.Cache[string]) map[string]string {
	entries := make(map[string]string)
	for _, login := range []string{"oldname", "newname", "other"} {
		value, found := c.Get("login:" + login)
		if found {
			entries[login] = value
		}
	}
	return entries
}

func TestUserIDCacheInvalidatesRenameKeys(t *testing.T) {
	hub := bus.NewInvalidations(nil, "", nil)
	c := seededUserIDs(t, hub)
	require.NoError(t, hub.Publish(invalidate.ScopeRename, 42, " OldName ", "NEWNAME"))
	require.Equal(t, map[string]string{"other": "84"}, cachedLogins(c))
}

func TestUserUpdateRenameInvalidatesEveryReplica(t *testing.T) {
	nc := testnats.Connect(t)
	owner := bus.NewInvalidations(nc, "cache.invalidate", nil)
	var replicas []*cache.Cache[string]
	for range 2 {
		hub := bus.NewInvalidations(nc, "cache.invalidate", nil)
		replicas = append(replicas, seededUserIDs(t, hub))
	}
	_, err := nc.Subscribe(testIdentitySubject, func(m *nats.Msg) {
		_ = owner.Publish(invalidate.ScopeRename, 42, "newname", "oldname")
		bus.Respond(m, usersrpc.IdentitySyncReply{Renamed: true, PriorUsername: "oldname"})
	})
	require.NoError(t, err)
	require.NoError(t, nc.Flush())
	w := &Worker{
		log: zap.NewNop(), userIDs: replicas[0],
		reauth: NewReauthNotifier(nc, ReauthConfig{IdentitySubject: testIdentitySubject}, zap.NewNop()),
	}
	require.NoError(t, w.HandleUserUpdate(&bus.Message{Payload: []byte(`{"user_id":"42","user_login":"newname","user_name":"NewName"}`)}))
	for _, c := range replicas {
		require.Eventually(t, func() bool {
			return reflect.DeepEqual(map[string]string{"other": "84"}, cachedLogins(c))
		}, time.Second, time.Millisecond)
	}
}
