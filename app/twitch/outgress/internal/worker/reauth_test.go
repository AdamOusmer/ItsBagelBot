// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package worker

import (
	"context"
	"testing"
	"time"

	"ItsBagelBot/internal/domain/rpc"
	notificationsrpc "ItsBagelBot/internal/domain/rpc/notifications"
	usersrpc "ItsBagelBot/internal/domain/rpc/users"
	"ItsBagelBot/internal/testnats"
	"ItsBagelBot/pkg/codec"

	"github.com/nats-io/nats.go"
	"go.uber.org/zap"
)

const (
	testSendSubject  = "test.notifications.send"
	testStateSubject = "test.users.state_get"
)

func newTestReauthNotifier(t *testing.T, nc *nats.Conn) *ReauthNotifier {
	t.Helper()
	return NewReauthNotifier(nc, ReauthConfig{
		SendSubject:   testSendSubject,
		StateSubject:  testStateSubject,
		ActiveSubject: "test.users.active_set",
		BotID:         "123",
	}, zap.NewNop())
}

func respondState(t *testing.T, nc *nats.Conn, locale string, code rpc.Code) {
	t.Helper()
	sub, err := nc.Subscribe(testStateSubject, func(msg *nats.Msg) {
		reply, _ := codec.Marshal(usersrpc.StateGetReply{Locale: locale, Refusal: rpc.Refusal{Code: code}})
		_ = msg.Respond(reply)
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sub.Unsubscribe() })
}

func captureSends(t *testing.T, nc *nats.Conn) *[]string {
	t.Helper()
	ids := &[]string{}
	sub, err := nc.Subscribe(testSendSubject, func(msg *nats.Msg) {
		var req notificationsrpc.SendRequest
		_ = codec.Unmarshal(msg.Data, &req)
		*ids = append(*ids, req.RequestID)
		reply, _ := codec.Marshal(notificationsrpc.SendReply{})
		_ = msg.Respond(reply)
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sub.Unsubscribe() })
	return ids
}

func TestNotifyKnownRequestIDTracksTheEpisode(t *testing.T) {
	tests := []struct {
		name          string
		firstEpisode  time.Time
		secondEpisode time.Time
		wantSameID    bool
	}{
		{
			name:          "two go-lives on the same unresolved block share an id",
			firstEpisode:  time.Unix(1700000000, 0),
			secondEpisode: time.Unix(1700000000, 0),
			wantSameID:    true,
		},
		{
			name:          "block, recover, block gets a new id",
			firstEpisode:  time.Unix(1700000000, 0),
			secondEpisode: time.Unix(1700100000, 0),
			wantSameID:    false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			nc := testnats.Connect(t)
			respondState(t, nc, "en", rpc.CodeOK)
			ids := captureSends(t, nc)
			r := newTestReauthNotifier(t, nc)

			r.Notify(context.Background(), "42", noticeBanned, tc.firstEpisode)
			r.Notify(context.Background(), "42", noticeBanned, tc.secondEpisode)
			mustFlush(t, nc)

			if len(*ids) != 2 {
				t.Fatalf("got %d sends, want 2", len(*ids))
			}
			gotSame := (*ids)[0] == (*ids)[1]
			if gotSame != tc.wantSameID {
				t.Errorf("request ids = %q, %q; want same=%v", (*ids)[0], (*ids)[1], tc.wantSameID)
			}
		})
	}
}

func TestNotifyKnownSkipsSendWhenAccountMissing(t *testing.T) {
	nc := testnats.Connect(t)
	respondState(t, nc, "en", rpc.CodeNotFound)
	ids := captureSends(t, nc)
	r := newTestReauthNotifier(t, nc)

	r.Notify(context.Background(), "42", noticeBanned, time.Unix(1700000000, 0))
	mustFlush(t, nc)

	if len(*ids) != 0 {
		t.Fatalf("got %d sends for a broadcaster with no account, want 0", len(*ids))
	}
}

func mustFlush(t *testing.T, nc *nats.Conn) {
	t.Helper()
	if err := nc.FlushTimeout(2 * time.Second); err != nil {
		t.Fatal(err)
	}
}
