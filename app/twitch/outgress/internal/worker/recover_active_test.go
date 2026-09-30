// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package worker

import (
	"context"
	"testing"

	usersrpc "ItsBagelBot/internal/domain/rpc/users"
	"ItsBagelBot/internal/testnats"
	"ItsBagelBot/pkg/codec"

	"github.com/nats-io/nats.go"
	"go.uber.org/zap"
)

func captureActiveSets(t *testing.T, nc *nats.Conn, subject string) *[]usersrpc.ActiveSetRequest {
	t.Helper()
	sets := &[]usersrpc.ActiveSetRequest{}
	sub, err := nc.Subscribe(subject, func(msg *nats.Msg) {
		var req usersrpc.ActiveSetRequest
		_ = codec.Unmarshal(msg.Data, &req)
		*sets = append(*sets, req)
		reply, _ := codec.Marshal(map[string]any{})
		_ = msg.Respond(reply)
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sub.Unsubscribe() })
	return sets
}

func TestRecoverActiveFlagOnlyWhenPriorStateWasBlocked(t *testing.T) {
	nc := testnats.Connect(t)
	const activeSubject = "test.users.active_set.recover"
	sets := captureActiveSets(t, nc, activeSubject)
	r := NewReauthNotifier(nc, ReauthConfig{ActiveSubject: activeSubject, BotID: "123"}, zap.NewNop())
	w := &Worker{log: zap.NewNop(), reauth: r}

	tests := []struct {
		name       string
		priorState string
		wantActive bool
	}{
		{"recovered from a chat ban", subStateBanned, true},
		{"recovered from a revoke", subStateRevoked, true},
		{"routine reconnect, never blocked", subStateOK, false},
		{"first-ever enroll, no prior state", "", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			*sets = nil
			w.recoverActiveFlag(context.Background(), enrollment{broadcasterID: "42", priorState: tc.priorState})
			mustFlush(t, nc)

			if tc.wantActive {
				if len(*sets) != 1 || !(*sets)[0].Active {
					t.Fatalf("active sets = %+v, want one active=true set", *sets)
				}
				return
			}
			if len(*sets) != 0 {
				t.Fatalf("active sets = %+v, want none", *sets)
			}
		})
	}
}
