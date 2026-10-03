// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package discordstore_test

import (
	"context"
	"reflect"
	"sync"
	"testing"

	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/require"

	"ItsBagelBot/internal/discordstore"
	ddiscord "ItsBagelBot/internal/domain/discord"
	"ItsBagelBot/internal/testnats"
	"ItsBagelBot/pkg/codec"
)

type verb string

type replies map[verb]any

type unsent struct{}

type scenario struct {
	name  string
	steps []step
}

type step struct {
	serve replies
	do    op
	want  any
	sent  replies
}

type store = discordstore.Store

type op func(context.Context, store) any

type discordData struct {
	nc   *nats.Conn
	subs []*nats.Subscription
	mu   sync.Mutex
	sent map[verb][]byte
}

func newRPCStore(t *testing.T) (store, *discordData) {
	t.Helper()
	nc := testnats.Connect(t)
	return discordstore.NewRPC(nc, discordstore.DefaultRPCPrefix, nil, nil), &discordData{nc: nc}
}

func (d *discordData) serve(t *testing.T, script replies) {
	t.Helper()
	if d == nil {
		return
	}
	for _, sub := range d.subs {
		require.NoError(t, sub.Unsubscribe())
	}
	d.subs = nil
	d.mu.Lock()
	d.sent = map[verb][]byte{}
	d.mu.Unlock()
	for v, reply := range script {
		body, err := codec.FastMarshal(reply)
		require.NoError(t, err)
		sub, err := d.nc.Subscribe(discordstore.DefaultRPCPrefix+"."+string(v), d.answer(v, body))
		require.NoError(t, err)
		d.subs = append(d.subs, sub)
	}
	require.NoError(t, d.nc.Flush())
}

func (d *discordData) answer(v verb, body []byte) nats.MsgHandler {
	return func(msg *nats.Msg) {
		d.mu.Lock()
		d.sent[v] = msg.Data
		d.mu.Unlock()
		_ = msg.Respond(body)
	}
}

func (d *discordData) requireSent(t *testing.T, want replies) {
	t.Helper()
	if d == nil {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	for v, request := range want {
		raw, ok := d.sent[v]
		if _, none := request.(unsent); none {
			require.False(t, ok, "%s reached discord-data", v)
			continue
		}
		got := reflect.New(reflect.TypeOf(request))
		require.NoError(t, codec.FastUnmarshal(raw, got.Interface()), "%s request", v)
		require.Equal(t, request, got.Elem().Interface(), "%s request", v)
	}
}

func runSteps(t *testing.T, s store, data *discordData, steps []step) {
	t.Helper()
	ctx := context.Background()
	for i, st := range steps {
		data.serve(t, st.serve)
		require.Equal(t, st.want, st.do(ctx, s), "step %d", i)
		data.requireSent(t, st.sent)
	}
}

var (
	g1    = discordstore.Guild{ID: "g1"}
	g2    = discordstore.Guild{ID: "g2"}
	c1    = discordstore.Channel{ID: "c1"}
	u1    = discordstore.Member{GuildID: "g1", UserID: "u1"}
	owner = discordstore.Broadcaster{ID: "77"}
	bound = discordstore.Binding{Guild: g1, Broadcaster: owner}
)

type found[T any] struct {
	V  T
	OK bool
}

func pair[T any](v T, ok bool) found[T] { return found[T]{V: v, OK: ok} }

type result[T any] struct {
	V   T
	Err error
}

func outcome[T any](v T, err error) result[T] { return result[T]{V: v, Err: err} }

type binding struct {
	ID     string
	Source discordstore.BindingSource
	OK     bool
}

type settings struct {
	Config  ddiscord.Config
	Version int
	OK      bool
}

type award struct {
	XP      int
	Leveled bool
	Level   int
}

type daily struct {
	Granted bool
	XP      int
}

type rank struct{ XP, Level int }

type seat struct {
	Left  string
	Empty bool
}

func call[A, R any](method func(store, context.Context, A) R, arg A) op {
	return func(ctx context.Context, s store) any { return method(s, ctx, arg) }
}

func callFound[A, R any](method func(store, context.Context, A) (R, bool), arg A) op {
	return func(ctx context.Context, s store) any { return pair(method(s, ctx, arg)) }
}

func callResult[A, R any](method func(store, context.Context, A) (R, error), arg A) op {
	return func(ctx context.Context, s store) any { return outcome(method(s, ctx, arg)) }
}

func readBinding(ctx context.Context, s store) any {
	b, source, ok := s.BindingOf(ctx, g1)
	return binding{ID: b.ID, Source: source, OK: ok}
}

func readConfig(ctx context.Context, s store) any {
	cfg, version, ok := s.GuildConfig(ctx, g1)
	return settings{Config: cfg, Version: version, OK: ok}
}

func invalidate(ctx context.Context, s store) any {
	s.Invalidate(ctx, g1)
	return nil
}

func readTicket(ch discordstore.Channel) op {
	return func(ctx context.Context, s store) any { return pair(s.Ticket(ctx, g1, ch)) }
}

func memTranscript(id int) op {
	return func(_ context.Context, s store) any { return pair(s.(*discordstore.Mem).Transcript(id)) }
}

func ticketsDurable(ctx context.Context, s store) any { return s.TicketsDurable(ctx) }

func addXP(ctx context.Context, s store) any {
	xp, leveled, level := s.AddXP(ctx, u1)
	return award{XP: xp, Leveled: leveled, Level: level}
}

func claimDaily(ctx context.Context, s store) any {
	granted, xp := s.ClaimDaily(ctx, u1)
	return daily{Granted: granted, XP: xp}
}

func readRank(ctx context.Context, s store) any {
	xp, level := s.Rank(ctx, u1)
	return rank{XP: xp, Level: level}
}

func moveVoice(to discordstore.VoiceSeat) op {
	return func(ctx context.Context, s store) any {
		left, empty := s.UpdateVoiceOccupancy(ctx, to)
		return seat{Left: left, Empty: empty}
	}
}
