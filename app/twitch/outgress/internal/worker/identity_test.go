// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package worker

import (
	"testing"

	"ItsBagelBot/internal/domain/rpc"
	usersrpc "ItsBagelBot/internal/domain/rpc/users"
	"ItsBagelBot/internal/testnats"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/codec"

	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

const testIdentitySubject = "test.users.identity_sync"

func TestHandleUserUpdateSyncsIdentity(t *testing.T) {
	nc := testnats.Connect(t)
	var got []usersrpc.IdentitySyncRequest
	reply := usersrpc.IdentitySyncReply{Renamed: true, PriorUsername: "old"}
	sub, err := nc.Subscribe(testIdentitySubject, func(m *nats.Msg) {
		var req usersrpc.IdentitySyncRequest
		_ = codec.Unmarshal(m.Data, &req)
		got = append(got, req)
		body, _ := codec.Marshal(reply)
		_ = m.Respond(body)
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = sub.Unsubscribe() })

	w := &Worker{log: zap.NewNop(), reauth: NewReauthNotifier(nc, ReauthConfig{IdentitySubject: testIdentitySubject}, zap.NewNop())}
	send := func(payload string) error { return w.HandleUserUpdate(&bus.Message{Payload: []byte(payload)}) }

	require.NoError(t, send(`{"user_id":"42","user_login":"newname","user_name":"NewName","event_id":"e1"}`))
	require.Equal(t, []usersrpc.IdentitySyncRequest{{UserID: "42", Username: "newname", DisplayName: "NewName"}}, got)

	got = nil
	require.NoError(t, send(`{"user_id":"","user_login":"x"}`))
	require.NoError(t, send(`{"user_id":"42"}`))
	require.NoError(t, send(`not json`))
	require.Empty(t, got, "malformed events must not reach the users service")

	reply = usersrpc.IdentitySyncReply{Refusal: rpc.Refused(rpc.CodeNotFound, "user account not found")}
	require.NoError(t, send(`{"user_id":"43","user_login":"gone","user_name":"Gone"}`))
	reply = usersrpc.IdentitySyncReply{Refusal: rpc.Refused(rpc.CodeInvalid, "bad login")}
	require.NoError(t, send(`{"user_id":"44","user_login":"bad login","user_name":"Bad"}`))
}

func TestHandleUserUpdateReturnsTransportError(t *testing.T) {
	nc := testnats.Connect(t)
	w := &Worker{log: zap.NewNop(), reauth: NewReauthNotifier(nc, ReauthConfig{IdentitySubject: "test.users.identity_sync.absent"}, zap.NewNop())}
	require.Error(t, w.HandleUserUpdate(&bus.Message{Payload: []byte(`{"user_id":"42","user_login":"a","user_name":"A"}`)}))
}
