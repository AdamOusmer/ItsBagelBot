// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package gateway

import (
	"context"
	"sync/atomic"
	"time"
)

// socket is one live connection plus the two channels its background
// goroutines share with the pump. It exists as a struct rather than three
// parameters because heartbeat, presenceLoop and handlePacket all need the
// same trio, and threading them separately is what CodeScene flags as an
// excess argument list.
//
// stop is closed by the pump when the socket ends, which is how the writer
// goroutines learn to exit. fail carries the first error a writer hit back
// the other way -- see firstError for why that direction matters.
type socket struct {
	conn Conn
	stop chan struct{}
	// fail is buffered for both writers (heartbeat and presenceLoop) so
	// neither can block on a send the pump never reads.
	fail chan error
	// inFlight counts writer goroutines currently inside Conn.Write. It is
	// what tells firstError whether a close code could still be on its way:
	// zero means no writer can produce one, so there is nothing to wait for.
	inFlight atomic.Int32
	// settled is signalled whenever the last in-flight write finishes, so a
	// waiting pump wakes on the writer's completion rather than serving out
	// the full grace period. Buffered and non-blocking: nobody may be
	// waiting, and a missed signal only costs the timer.
	settled chan struct{}
	// acks counts the heartbeat ACKs (op 11) Discord has answered on this
	// socket. Written by the pump goroutine, read by the heartbeat's, which
	// is why it is atomic rather than a plain int.
	acks atomic.Int64
}

// noteAck records one heartbeat ACK.
func (k *socket) noteAck() { k.acks.Add(1) }

// stale reports whether the heartbeat written on the previous tick went
// unanswered. sent is how many this socket has written so far.
//
// This is the only test that separates a live socket from a zombied one. A
// TCP connection that Discord has stopped serving stays open and readable
// forever; nothing arrives, no close frame is sent, and the pump waits in
// Read for the life of the process. Discord's own docs name the case and say
// the client must close and resume. Until this existed, ingress had no
// detection at all: on 2026-09-07 the surviving evidence of a broken gateway
// was 21,575 reconnect lines with no cause on any of them.
//
// A count, not a deadline. The ACK for beat N is due before beat N+1 goes
// out, and comparing counters says exactly that, where a duration threshold
// would have to guess a slack around an interval Discord picks per session
// (41.25s today, documented as not constant). It is also why no read deadline
// is set on the connection: there has never been one, and one would be a
// second, coarser copy of this same test -- a read deadline cannot tell a
// quiet guild from a dead gateway, which is the entire distinction an ACK
// exists to draw.
func (k *socket) stale(sent int64) bool { return k.acks.Load() < sent }

func newSocket(conn Conn) *socket {
	return &socket{
		conn:    conn,
		stop:    make(chan struct{}),
		fail:    make(chan error, 2),
		settled: make(chan struct{}, 1),
	}
}

// writerGrace bounds how long the pump waits for a writer that is mid-Write
// to report its own error before concluding the socket died without a close
// code.
//
// The wait exists because the two failures race. Discord's close frame
// surfaces on whichever operation touches the socket next, and when a writer
// gets it, that writer closes the connection -- which makes the pump's
// parked Read return a generic, codeless "use of closed network connection"
// microseconds BEFORE the Write it was racing has returned anything at all.
// The non-blocking poll firstError used to do found an empty fail channel,
// reported no code, and a 4004 that can never be reconnected went back into
// the 1s..60s backoff. That is one of the two loops that got this bot's
// token reset (see budget.go).
//
// 500ms because the wait is only ever paid on a socket that is already dead:
// the writer either lands within a scheduling quantum of the read (the whole
// window here is a Write returning, measured in microseconds) or it is stuck
// in a TCP send on a connection nobody is draining, in which case no code is
// coming and the reconnect should get on with it. It is short enough to be
// invisible next to the 1s backoff floor that follows and long enough that a
// loaded node's scheduler cannot lose the race for us. The wait ends early on
// the writer's completion anyway, so the full 500ms is the pathological case,
// not the normal one.
const writerGrace = 500 * time.Millisecond

// write sends one frame from a writer goroutine, funnelling a failure back
// to the pump and keeping the in-flight count honest.
//
// The ordering is load-bearing: writeFailed puts the error on fail BEFORE
// the count drops to zero and settled fires, so a pump woken by settled can
// poll fail once and trust the answer.
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

// writeFailed records a writer goroutine's error and closes the connection.
//
// The close is not tidiness: the pump is parked in Conn.Read, and a socket
// whose writes fail may still never deliver another frame, so without this
// the pump waits out Discord's own timeout before noticing. Closing makes
// that Read return now; firstError is what makes sure the error the caller
// finally sees is this one and not the generic "use of closed connection"
// the Read reports.
func (k *socket) writeFailed(err error) {
	select {
	case k.fail <- err:
	default:
	}
	_ = k.conn.Close()
}

// firstError prefers a writer goroutine's error over the pump's own.
//
// This is the whole reason fail exists. Discord's fatal closes (4004 bad
// token, 4013/4014 intents) arrive as a close frame, and the frame surfaces
// on whichever operation touches the socket next. When that is the
// heartbeat's Write rather than the pump's Read, the pump saw only a closed
// connection -- Conn.CloseCode reads no code off that, discord.FatalCloseCode
// says false, and a token that will never work again gets retried forever on
// a 1s..60s backoff. Routing the writer's error back here puts the real
// close code in front of CloseCode.
func (k *socket) firstError(err error) error {
	if werr := k.pollFail(); werr != nil {
		return werr
	}
	if k.conn.CloseCode(err) != 0 {
		// The pump read a real close frame. Whatever a writer is doing
		// cannot be a better answer than the code Discord actually sent.
		return err
	}
	return k.awaitWriterError(err)
}

// awaitWriterError gives a writer that is still inside Write the chance to
// report the close frame it is about to see, rather than concluding from an
// empty channel that the socket died codeless. See writerGrace.
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
		// Every writer finished. Its failure, if it had one, was put on
		// fail before this signal (see write).
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
