// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package main

import (
	"context"
	"net"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"ItsBagelBot/internal/domain/event/data"
	domainrpc "ItsBagelBot/internal/domain/rpc"
	loyaltyrpc "ItsBagelBot/internal/domain/rpc/loyalty"
	"ItsBagelBot/internal/projection"
	"ItsBagelBot/internal/testnats"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/codec"

	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/require"
	"github.com/valkey-io/valkey-go"
)

type fakeLiveStore struct {
	outcome   projection.LiveOutcome
	applyErr  error
	batches   []projection.LiveBatch
	seededAt  time.Time
	totals    map[uint64]map[projection.CounterName]int64
	boardDone bool
	boards    map[projection.CounterName][]projection.BoardEntry
	deleted   int
}

func newFakeLiveStore() *fakeLiveStore {
	return &fakeLiveStore{totals: map[uint64]map[projection.CounterName]int64{}, boards: map[projection.CounterName][]projection.BoardEntry{}}
}

func (f *fakeLiveStore) ApplyLiveCounters(_ context.Context, b projection.LiveBatch) (projection.LiveOutcome, error) {
	f.batches = append(f.batches, b)
	return f.outcome, f.applyErr
}

func (f *fakeLiveStore) SeedLiveCounters(_ context.Context, userID uint64, values []projection.CounterValue, seededAt time.Time) error {
	f.seededAt = seededAt
	f.totals[userID] = map[projection.CounterName]int64{}
	for _, v := range values {
		f.totals[userID][v.Name] = v.Value
	}
	return nil
}

func (f *fakeLiveStore) GetLiveCounters(_ context.Context, userID uint64, _ []projection.CounterName) (map[projection.CounterName]int64, bool, error) {
	got, ok := f.totals[userID]
	return got, ok, nil
}

func (f *fakeLiveStore) BoardSeeded(context.Context, projection.CounterName) (bool, error) {
	return f.boardDone, nil
}

func (f *fakeLiveStore) SeedBoard(_ context.Context, name projection.CounterName, entries []projection.BoardEntry) error {
	f.boards[name] = entries
	return nil
}

func (f *fakeLiveStore) DeleteLiveCounters(context.Context, uint64, []projection.CounterName) error {
	f.deleted++
	return nil
}

type loyaltyMode int

const (
	loyaltyServes loyaltyMode = iota
	loyaltyRefuses
	loyaltyMalformed
	loyaltyAbsent
)

type loyaltyService struct {
	values map[string]int64
	board  []loyaltyrpc.CounterRank
	reads  atomic.Int32
}

func newLoyalty(t *testing.T, mode loyaltyMode, values map[string]int64, board []loyaltyrpc.CounterRank) (*loyaltyService, *loyaltyCounters) {
	t.Helper()
	const prefix = "bagel.rpc.loyalty"
	service := &loyaltyService{values: values, board: board}
	nc := testnats.Connect(t)
	if mode != loyaltyAbsent {
		_, err := nc.Subscribe(prefix+".counter.*", service.serve(mode))
		require.NoError(t, err)
		require.NoError(t, nc.Flush())
	}
	return service, newLoyaltyCounters(nc, prefix)
}

func (s *loyaltyService) serve(mode loyaltyMode) nats.MsgHandler {
	return func(msg *nats.Msg) {
		s.reads.Add(1)
		var req loyaltyrpc.Request
		_ = codec.Unmarshal(msg.Data, &req)
		_ = msg.Respond(s.reply(mode, strings.HasSuffix(msg.Subject, ".board"), req))
	}
}

func (s *loyaltyService) reply(mode loyaltyMode, board bool, req loyaltyrpc.Request) []byte {
	switch {
	case mode == loyaltyMalformed:
		return []byte("not json")
	case mode == loyaltyRefuses:
		body, _ := codec.Marshal(loyaltyrpc.Reply{Refusal: domainrpc.Refused(domainrpc.CodeInvalid, "bad request")})
		return body
	case board:
		body, _ := codec.Marshal(loyaltyrpc.Reply{Board: s.board})
		return body
	}
	value, found := s.values[req.Name]
	body, _ := codec.Marshal(loyaltyrpc.Reply{Found: found, Counter: &loyaltyrpc.Counter{Name: req.Name, Value: value}})
	return body
}

func bump(name, scope string, delta int64) data.CounterBumpEntry {
	return data.CounterBumpEntry{Name: name, Scope: scope, Delta: delta}
}

func message(t *testing.T, id string, payload any) *bus.Message {
	t.Helper()
	body, err := codec.Marshal(payload)
	require.NoError(t, err)
	return &bus.Message{UUID: id, Payload: body}
}

func testValkey(t *testing.T) valkey.Client {
	t.Helper()
	binary, err := exec.LookPath("valkey-server")
	if err != nil {
		t.Skip("valkey-server is not installed")
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	address := listener.Addr().String()
	port := listener.Addr().(*net.TCPAddr).Port
	require.NoError(t, listener.Close())
	dir := t.TempDir()
	server := exec.Command(binary, "--bind", "127.0.0.1", "--port", strconv.Itoa(port), "--save", "", "--appendonly", "no", "--dir", dir, "--logfile", filepath.Join(dir, "valkey.log"))
	require.NoError(t, server.Start())
	t.Cleanup(func() { _ = server.Process.Kill(); _ = server.Wait() })
	require.Eventually(t, func() bool {
		conn, err := net.DialTimeout("tcp", address, 50*time.Millisecond)
		if err != nil {
			return false
		}
		_ = conn.Close()
		return true
	}, 3*time.Second, 10*time.Millisecond)
	client, err := valkey.NewClient(valkey.ClientOption{InitAddress: []string{address}, DisableCache: true})
	require.NoError(t, err)
	t.Cleanup(client.Close)
	return client
}
