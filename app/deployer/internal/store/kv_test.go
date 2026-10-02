// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package store

import (
	"context"
	"testing"
	"time"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ItsBagelBot/app/deployer/internal/ports"
	"ItsBagelBot/internal/domain/rpc/deploy"
)

func jetStreamStore(t *testing.T) *Store {
	t.Helper()
	srv, err := server.NewServer(&server.Options{Host: "127.0.0.1", Port: -1, NoLog: true, NoSigs: true, JetStream: true, StoreDir: t.TempDir()})
	require.NoError(t, err)
	srv.Start()
	t.Cleanup(func() {
		srv.Shutdown()
		srv.WaitForShutdown()
	})
	require.True(t, srv.ReadyForConnections(5*time.Second), "NATS server did not become ready")
	nc, err := nats.Connect(srv.ClientURL(), nats.NoReconnect())
	require.NoError(t, err)
	t.Cleanup(nc.Close)
	js, err := jetstream.New(nc)
	require.NoError(t, err)
	kv, err := js.CreateKeyValue(context.Background(), jetstream.KeyValueConfig{Bucket: deploy.KVBucket, History: 1})
	require.NoError(t, err)
	return newStore(jsBucket{kv: kv}, testLockTTL, ports.SystemClock{})
}

func TestJetStreamBucketMapsErrorsOntoPortsSentinels(t *testing.T) {
	ctx := context.Background()
	done := func(id deploy.RunID) *deploy.Run { return &deploy.Run{ID: id, State: deploy.RunSucceeded} }
	cases := []struct {
		name string
		do   func(*Store) error
		want error
	}{
		{"reading a run never written is not found", func(s *Store) error {
			_, _, err := s.Get(ctx, "nope")
			return err
		}, ports.ErrNotFound},
		{"writing a run twice as new conflicts", func(s *Store) error {
			_, _ = s.Put(ctx, done("twice"), 0)
			_, err := s.Put(ctx, done("twice"), 0)
			return err
		}, ports.ErrConflict},
		{"writing at a stale revision conflicts", func(s *Store) error {
			first, _ := s.Put(ctx, done("stale"), 0)
			_, _ = s.Put(ctx, done("stale"), first)
			_, err := s.Put(ctx, done("stale"), first)
			return err
		}, ports.ErrConflict},
		{"a run id that is no valid key is invalid", func(s *Store) error {
			_, err := s.Put(ctx, done("bad id"), 0)
			return err
		}, ports.ErrInvalid},
	}
	s := jetStreamStore(t)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.ErrorIs(t, tc.do(s), tc.want)
		})
	}
}
