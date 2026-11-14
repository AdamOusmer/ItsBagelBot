// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"context"
	"testing"
	"time"

	"ItsBagelBot/app/db/transactions/mail"
	domainrpc "ItsBagelBot/internal/domain/rpc"
	transactionsrpc "ItsBagelBot/internal/domain/rpc/transactions"
	usersrpc "ItsBagelBot/internal/domain/rpc/users"
	"ItsBagelBot/internal/testnats"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/env/conf"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type recordingGrantMailer struct {
	sent []mail.GrantMessage
	err  error
}

func (m *recordingGrantMailer) SendGrant(_ context.Context, msg mail.GrantMessage) error {
	m.sent = append(m.sent, msg)
	return m.err
}

func TestGrantEmailRPC(t *testing.T) {
	expires := time.Now().Add(30 * 24 * time.Hour).UTC().Truncate(time.Second)
	contacts := map[string]usersrpc.EmailGetReply{
		"21": {Email: "streamer@example.com", Locale: "de"},
		"22": {},
	}
	for _, tc := range []struct {
		name    string
		req     transactionsrpc.GrantEmailRequest
		sendErr error
		outcome transactionsrpc.GrantEmailOutcome
		code    domainrpc.Code
		sent    []mail.GrantMessage
	}{
		{
			name:    "sends in the recipient locale with a per-grant key",
			req:     transactionsrpc.GrantEmailRequest{ActorID: "10", UserID: "21", ExpiresAt: expires},
			outcome: transactionsrpc.GrantEmailSent,
			sent:    []mail.GrantMessage{{To: "streamer@example.com", Locale: "de", ExpiresAt: expires, IdempotencyKey: grantEmailKey(21, expires)}},
		},
		{
			name:    "reports a missing contact email without sending",
			req:     transactionsrpc.GrantEmailRequest{ActorID: "10", UserID: "22", ExpiresAt: expires},
			outcome: transactionsrpc.GrantEmailNoEmail,
		},
		{
			name: "refuses a non-admin actor",
			req:  transactionsrpc.GrantEmailRequest{ActorID: "11", UserID: "21", ExpiresAt: expires},
			code: domainrpc.CodeForbidden,
		},
		{
			name: "refuses a past end date",
			req:  transactionsrpc.GrantEmailRequest{ActorID: "10", UserID: "21", ExpiresAt: time.Now().Add(-time.Hour)},
			code: domainrpc.CodeInvalid,
		},
		{
			name:    "surfaces a provider failure",
			req:     transactionsrpc.GrantEmailRequest{ActorID: "10", UserID: "21", ExpiresAt: expires},
			sendErr: mail.ErrDisabled,
			code:    domainrpc.CodeUnavailable,
			sent:    []mail.GrantMessage{{To: "streamer@example.com", Locale: "de", ExpiresAt: expires, IdempotencyKey: grantEmailKey(21, expires)}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			nc := testnats.Connect(t)
			w := bus.RPCWiring{NC: nc, Log: zap.NewNop()}
			require.NoError(t, bus.Serve(w, defaultUsersAuthSubject, func(_ context.Context, req usersrpc.AuthRequest) usersrpc.AuthReply {
				return usersrpc.AuthReply{Admin: req.UserID == "10", Role: "admin"}
			}))
			require.NoError(t, bus.Serve(w, conf.UsersEmailSubject.Get(), func(_ context.Context, req usersrpc.EmailGetRequest) usersrpc.EmailGetReply {
				return contacts[req.UserID]
			}))
			mailer := &recordingGrantMailer{err: tc.sendErr}
			require.NoError(t, SubscribeGrantEmail(w, GrantEmailConfig{Users: NewUsersGiveawayClient(nc), Mailer: mailer}))

			reply := giveawayRequest[transactionsrpc.GrantEmailReply](t, nc, transactionsrpc.GrantEmailSubject, tc.req)
			assert.Equal(t, tc.outcome, reply.Outcome)
			assert.Equal(t, tc.code, reply.Code)
			assert.Equal(t, tc.sent, mailer.sent)
		})
	}
}
