// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package gateway

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
}

func newSocket(conn Conn) *socket {
	return &socket{conn: conn, stop: make(chan struct{}), fail: make(chan error, 2)}
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
	select {
	case werr := <-k.fail:
		if werr != nil {
			return werr
		}
	default:
	}
	return err
}
