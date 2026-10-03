// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package presence_test

import (
	"context"
	"errors"
	"testing"

	"ItsBagelBot/app/discord/ingress/internal/presence"
	"ItsBagelBot/internal/testnats"
	"ItsBagelBot/pkg/bus"

	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type refresh struct {
	forget bool
	total  int
	err    error
}

type sent struct {
	Name string
	OK   bool
}

func refreshAll(steps []refresh) []sent {
	next := 0
	s := &presence.Source{Fetch: func(context.Context) (int, error) {
		step := steps[next]
		return step.total, step.err
	}}
	var out []sent
	for i, step := range steps {
		next = i
		if step.forget {
			s.Forget()
		}
		name, ok := s.Refresh(context.Background())
		out = append(out, sent{Name: name, OK: ok})
	}
	return out
}

func TestRefreshSendsOnlyChangedCounts(t *testing.T) {
	unreachable := errors.New("users service unreachable")
	cases := []struct {
		name  string
		steps []refresh
		want  []sent
	}{
		{
			name:  "sends the first count and every change",
			steps: []refresh{{total: 5}, {total: 9}},
			want:  []sent{{"5 streams", true}, {"9 streams", true}},
		},
		{
			name:  "skips an unchanged count",
			steps: []refresh{{total: 42}, {total: 42}},
			want:  []sent{{"42 streams", true}, {}},
		},
		{
			name:  "forget resends the unchanged count after a reconnect",
			steps: []refresh{{total: 7}, {total: 7}, {forget: true, total: 7}},
			want:  []sent{{"7 streams", true}, {}, {"7 streams", true}},
		},
		{
			name:  "failed fetch keeps the previous status",
			steps: []refresh{{total: 3}, {err: unreachable}, {total: 3}},
			want:  []sent{{"3 streams", true}, {}, {}},
		},
		{
			name:  "pluralizes and groups the count",
			steps: []refresh{{total: 0}, {total: 1}, {total: 2}, {total: 1234}},
			want:  []sent{{"0 streams", true}, {"1 stream", true}, {"2 streams", true}, {"1,234 streams", true}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, refreshAll(tc.steps))
		})
	}
}

type fetched struct {
	Total  int
	Failed bool
}

func answer(t *testing.T, nc *nats.Conn, subject string, reply []byte) {
	t.Helper()
	if reply == nil {
		return
	}
	require.NoError(t, bus.QueueSubscribeRPC(nc, subject, "users", func(m *nats.Msg) {
		_ = m.Respond(reply)
	}))
	require.NoError(t, nc.Flush())
}

func TestNewFetchAsksTheUsersServiceForTheTotal(t *testing.T) {
	nc := testnats.Connect(t)
	cases := []struct {
		name    string
		subject string
		reply   []byte
		want    fetched
	}{
		{"returns the total users", "users.counts.ok", []byte(`{"total_users":1234,"active_users":3}`), fetched{Total: 1234}},
		{"surfaces a refusal as an error", "users.counts.refused", []byte(`{"error":"users service unavailable"}`), fetched{Failed: true}},
		{"fails when nobody answers", "users.counts.none", nil, fetched{Failed: true}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			answer(t, nc, tc.subject, tc.reply)

			total, err := presence.NewFetch(nc, tc.subject)(context.Background())

			assert.Equal(t, tc.want, fetched{Total: total, Failed: err != nil})
		})
	}
}
