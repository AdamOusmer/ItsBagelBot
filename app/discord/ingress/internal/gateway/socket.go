// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package gateway

import (
	"context"
	"sync/atomic"
	"time"
)

type socket struct {
	conn     Conn
	stop     chan struct{}
	fail     chan error
	inFlight atomic.Int32
	settled  chan struct{}
	acks     atomic.Int64
}

func (k *socket) noteAck() { k.acks.Add(1) }

func (k *socket) stale(sent int64) bool { return k.acks.Load() < sent }

func newSocket(conn Conn) *socket {
	return &socket{
		conn:    conn,
		stop:    make(chan struct{}),
		fail:    make(chan error, 2),
		settled: make(chan struct{}, 1),
	}
}

const writerGrace = 500 * time.Millisecond

func (k *socket) write(ctx context.Context, v any) error {
	k.inFlight.Add(1)
	err := writeJSON(ctx, k.conn, v)
	if err != nil {
		k.writeFailed(err)
	}
	if k.inFlight.Add(-1) == 0 {
		select {
		case k.settled <- struct{}{}:
		default:
		}
	}
	return err
}

func (k *socket) writeFailed(err error) {
	select {
	case k.fail <- err:
	default:
	}
	_ = k.conn.Close()
}

func (k *socket) firstError(err error) error {
	if werr := k.pollFail(); werr != nil {
		return werr
	}
	if k.conn.CloseCode(err) != 0 {
		return err
	}
	return k.awaitWriterError(err)
}

func (k *socket) awaitWriterError(err error) error {
	if k.inFlight.Load() == 0 {
		return err
	}
	t := time.NewTimer(writerGrace)
	defer t.Stop()
	select {
	case werr := <-k.fail:
		if werr != nil {
			return werr
		}
	case <-k.settled:
		if werr := k.pollFail(); werr != nil {
			return werr
		}
	case <-t.C:
	}
	return err
}

func (k *socket) pollFail() error {
	select {
	case werr := <-k.fail:
		return werr
	default:
		return nil
	}
}
