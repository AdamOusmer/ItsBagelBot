// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package bus

import (
	"testing"

	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func closedConn(t *testing.T) *nats.Conn {
	t.Helper()
	nc, err := nats.Connect(brokerURL(t))
	require.NoError(t, err)
	nc.Close()
	return nc
}

func TestConcurrentClosersAllSeeTheShutdownError(t *testing.T) {
	tests := []struct {
		name string
		open func(*nats.Conn) interface{ Close() error }
	}{
		{"durable subscriber", func(nc *nats.Conn) interface{ Close() error } {
			return newConcurrentDurableSubscriber(concurrentSubscriberConfig{nc: nc, log: zap.NewNop()})
		}},
		{"pull subscriber", func(nc *nats.Conn) interface{ Close() error } {
			heartbeatDone := make(chan struct{})
			close(heartbeatDone)
			return &pullSubscriber{
				nc: nc, log: zap.NewNop(), output: make(chan *Message), closeCh: make(chan struct{}),
				heartbeatStop: make(chan struct{}), heartbeatDone: heartbeatDone,
			}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sub := tt.open(closedConn(t))
			results := make(chan error, 2)
			for range 2 {
				go func() { results <- sub.Close() }()
			}
			for range 2 {
				require.ErrorIs(t, <-results, nats.ErrConnectionClosed)
			}
		})
	}
}
