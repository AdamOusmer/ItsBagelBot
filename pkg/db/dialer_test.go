// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package db

import (
	"context"
	"errors"
	"net"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/stretchr/testify/require"
)

var errConnRefused = errors.New("connect: connection refused")

const stallAfterAttempts = 100

type dialStep int

const (
	dialRefused dialStep = iota
	dialBlocks
	dialConnects
)

type scriptedDial struct {
	mu       sync.Mutex
	steps    []dialStep
	conn     net.Conn
	attempts int
}

func newScriptedDial(t *testing.T, steps ...dialStep) *scriptedDial {
	t.Helper()

	client, server := net.Pipe()
	t.Cleanup(func() {
		_ = client.Close()
		_ = server.Close()
	})
	return &scriptedDial{steps: steps, conn: client}
}

func (s *scriptedDial) nextStep() dialStep {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.attempts++
	if s.attempts > stallAfterAttempts {
		return dialBlocks
	}
	return s.steps[min(s.attempts, len(s.steps))-1]
}

func (s *scriptedDial) attemptCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.attempts
}

func (s *scriptedDial) dial(ctx context.Context, _, _ string) (net.Conn, error) {
	switch s.nextStep() {
	case dialConnects:
		return s.conn, nil
	case dialBlocks:
		<-ctx.Done()
		return nil, ctx.Err()
	default:
		return nil, errConnRefused
	}
}

func (s *scriptedDial) dialer() retryDialer {
	return retryDialer{attempt: s.dial, perAttempt: dialAttemptTimeout}
}

func TestRetryDialer(t *testing.T) {
	cases := []struct {
		name         string
		steps        []dialStep
		cancelAfter  time.Duration
		wantErr      error
		wantAttempts int
		wantElapsed  time.Duration
	}{
		{name: "retries past refused and stalled attempts", steps: []dialStep{dialRefused, dialBlocks, dialConnects}, wantAttempts: 3, wantElapsed: 2 * dialAttemptTimeout},
		{name: "parent cancel stops retrying", steps: []dialStep{dialBlocks}, cancelAfter: 2 * time.Second, wantErr: context.Canceled, wantAttempts: 2, wantElapsed: 2 * time.Second},
		{name: "overall timeout stops stalled attempts", steps: []dialStep{dialBlocks}, wantErr: context.DeadlineExceeded, wantAttempts: 7, wantElapsed: dialTimeout},
		{name: "refused attempts wait out their window", steps: []dialStep{dialRefused}, wantErr: errConnRefused, wantAttempts: 7, wantElapsed: dialTimeout},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				script := newScriptedDial(t, tc.steps...)
				ctx, cancel := context.WithTimeout(t.Context(), dialTimeout)
				defer cancel()
				if tc.cancelAfter > 0 {
					time.AfterFunc(tc.cancelAfter, cancel)
				}
				start := time.Now()

				conn, err := script.dialer().DialContext(ctx, "tcp", testDBAddr)

				require.ErrorIs(t, err, tc.wantErr)
				require.Equal(t, tc.wantErr == nil, conn != nil)
				require.Equal(t, tc.wantAttempts, script.attemptCount())
				require.Equal(t, tc.wantElapsed, time.Since(start))
			})
		})
	}
}

func TestMySQLConnectorCapsRetryDialerAtDialTimeout(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		script := newScriptedDial(t, dialBlocks)
		mc := newMySQLConfig(Config{Address: testDBAddr, Schema: "bagel_test"})
		mc.DialFunc = script.dialer().DialContext
		connector, err := mysql.NewConnector(mc)
		require.NoError(t, err)
		start := time.Now()

		_, err = connector.Connect(context.Background())

		require.ErrorIs(t, err, context.DeadlineExceeded)
		require.Equal(t, dialTimeout, time.Since(start))
		require.Equal(t, 7, script.attemptCount())
	})
}
