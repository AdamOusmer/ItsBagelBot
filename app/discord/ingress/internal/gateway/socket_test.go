// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package gateway

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	ddiscord "ItsBagelBot/internal/domain/discord"
	"ItsBagelBot/pkg/codec"

	"github.com/coder/websocket"
)

// TestWSConnCloseCodeReadsRealFrames pins the translation the whole fatal
// path rests on. wsConn.CloseCode is the only thing that turns a websocket
// error into the number discord.FatalCloseCode judges, and its library
// returns -1 (not 0) for "not a close frame", which is the sentinel this
// folds away.
func TestWSConnCloseCodeReadsRealFrames(t *testing.T) {
	var w wsConn
	cases := []struct {
		name string
		err  error
		want int
	}{
		{
			// The one that mattered on 2026-09-05: a rejected token.
			name: "close frame",
			err:  websocket.CloseError{Code: websocket.StatusCode(ddiscord.CloseAuthenticationFailed), Reason: "Authentication failed."},
			want: ddiscord.CloseAuthenticationFailed,
		},
		{
			// Wrapped is the normal case: the error crosses readPacket and
			// pump before anything asks for its code.
			name: "wrapped close frame",
			err:  fmt.Errorf("read packet: %w", websocket.CloseError{Code: websocket.StatusCode(ddiscord.CloseDisallowedIntents)}),
			want: ddiscord.CloseDisallowedIntents,
		},
		{
			name: "plain network error",
			err:  &net.OpError{Op: "read", Err: errors.New("connection reset by peer")},
			want: 0,
		},
		{
			name: "context cancellation",
			err:  context.Canceled,
			want: 0,
		},
		{
			name: "nil",
			err:  nil,
			want: 0,
		},
	}
	for _, tc := range cases {
		if got := w.CloseCode(tc.err); got != tc.want {
			t.Fatalf("%s: CloseCode = %d, want %d", tc.name, got, tc.want)
		}
	}
	// A normal closure is a real code and must survive as one: it is not
	// fatal, but it is not "no code" either.
	if got := w.CloseCode(websocket.CloseError{Code: websocket.StatusNormalClosure}); got != int(websocket.StatusNormalClosure) {
		t.Fatalf("CloseCode(normal closure) = %d, want %d", got, websocket.StatusNormalClosure)
	}
}

// The reconnecting close only works if the library will actually send it:
// coder/websocket refuses close codes outside the wire-valid set, and a
// refused Close writes no frame at all, which leaves the peer to time the
// session out instead of being told the transport went. This dials a real
// socket rather than reading close.go, and asserts the peer sees the private
// -range code and not 1000.
func TestReconnectingCloseReachesThePeer(t *testing.T) {
	seen := make(chan websocket.StatusCode, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		_, _, rerr := c.Read(r.Context())
		seen <- websocket.CloseStatus(rerr)
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn, err := DialWS(ctx, "ws"+strings.TrimPrefix(srv.URL, "http"))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	if err := conn.Close(); err != nil {
		t.Fatalf("reconnecting close: %v", err)
	}

	if code := <-seen; code != reconnectingClose {
		t.Fatalf("peer saw close code %d, want %d", code, reconnectingClose)
	}
}

// authFailure is the close frame Discord sends for a rejected token.
func authFailure() error {
	return websocket.CloseError{
		Code:   websocket.StatusCode(ddiscord.CloseAuthenticationFailed),
		Reason: "Authentication failed.",
	}
}

// heartbeatFailingConn is a socket that identifies fine and then dies on its
// first heartbeat write, which is exactly how a 4004 reached production:
// Discord's close frame surfaced on the write, the pump saw only a closed
// connection, and the code never reached the fatal check.
func heartbeatFailingConn(t *testing.T) *scriptedConn {
	t.Helper()
	hello, err := fastHello()
	if err != nil {
		t.Fatalf("marshal hello: %v", err)
	}
	return &scriptedConn{
		reads:         [][]byte{hello},
		writeErr:      authFailure(),
		writeErrAfter: 1, // the Identify goes through; the heartbeat does not
		closed:        make(chan struct{}),
	}
}

// oneSocketOver runs a single socket over conn under a bounded context and
// reports what it ended with. Every socket-level case below shares this exact
// wiring; the interesting difference between them is the connection, so it is
// the only thing they say.
func oneSocketOver(t *testing.T, conn Conn, timeout time.Duration) sessionEnd {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	sess := Session{Token: "t", Dial: func(context.Context, string) (Conn, error) { return conn, nil }}
	return sess.oneSocket(ctx, "ws://x", &resumeState{})
}

// wantCloseFrame asserts the error that ended a socket is Discord's close
// frame and not the pump's generic read error -- the close code only survives
// on the frame, and a codeless error reconnects forever.
func wantCloseFrame(t *testing.T, err error) {
	t.Helper()
	if !errors.As(err, &websocket.CloseError{}) {
		t.Fatalf("err = %v, want the close frame, not the pump's generic read error", err)
	}
}

// TestWriteCloseCodeReachesTheFatalPath is the regression test for the
// token reset: the code must come back off a write error, not off the
// generic "closed connection" the pump reads afterwards.
func TestWriteCloseCodeReachesTheFatalPath(t *testing.T) {
	end := oneSocketOver(t, heartbeatFailingConn(t), time.Second)
	code, err := end.code, end.err

	if code != ddiscord.CloseAuthenticationFailed {
		t.Fatalf("close code = %d, want %d off the write error", code, ddiscord.CloseAuthenticationFailed)
	}
	if !ddiscord.FatalCloseCode(code) {
		t.Fatalf("code %d must be fatal", code)
	}
	wantCloseFrame(t, err)
}

// And the loop above it must act on that: one dial, then park. Before the
// fix this reconnected every 2s forever, which is what got the token reset.
func TestRunParksOnAWriteSideFatalClose(t *testing.T) {
	dials := 0
	dial := func(context.Context, string) (Conn, error) {
		dials++
		return heartbeatFailingConn(t), nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	st := &recStatus{}
	sess := Session{Token: "t", Dial: dial, Status: st}

	if err := sess.Run(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Run err = %v, want the context error (Run must park)", err)
	}
	if dials != 1 {
		t.Fatalf("dials = %d, want exactly 1", dials)
	}
	wantFatalDown(t, st, ddiscord.CloseAuthenticationFailed)
}

// fastHello is a HELLO with a heartbeat interval short enough that the
// heartbeat goroutine fires inside a test window.
func fastHello() ([]byte, error) {
	d, err := codec.Marshal(helloData{HeartbeatInterval: 10})
	if err != nil {
		return nil, err
	}
	return codec.Marshal(packet{Op: opHello, D: d})
}

// ackingConn is a socket that answers op 11 acks times and then goes quiet
// without ever closing -- the zombie shape. A live TCP connection that
// Discord has stopped serving reads forever, so this fake never returns an
// error either: only the heartbeat's own ACK accounting can end it.
func ackingConn(t *testing.T, acks int) *scriptedConn {
	t.Helper()
	hello, err := fastHello()
	if err != nil {
		t.Fatalf("marshal hello: %v", err)
	}
	ack, err := codec.Marshal(packet{Op: opHeartbeatAck})
	if err != nil {
		t.Fatalf("marshal ack: %v", err)
	}
	reads := [][]byte{hello}
	for range acks {
		reads = append(reads, ack)
	}
	return &scriptedConn{reads: reads, closed: make(chan struct{})}
}

// A gateway that stops answering heartbeats leaves a socket that is open,
// readable and delivering nothing. Before this the pump sat in Read for the
// life of the process with the status key still claiming the bot was up.
func TestHeartbeatEndsASocketThatStopsAcking(t *testing.T) {
	end := oneSocketOver(t, ackingConn(t, 0), 2*time.Second)

	if !errors.Is(end.err, errZombie) {
		t.Fatalf("socket ended with %v, want the unacknowledged-heartbeat error", end.err)
	}
}

// And a socket that does answer must be left alone: the ACK accounting is
// only allowed to kill a connection Discord has actually abandoned.
func TestHeartbeatKeepsASocketThatAcks(t *testing.T) {
	end := oneSocketOver(t, ackingConn(t, 50), 200*time.Millisecond)

	if !errors.Is(end.err, context.DeadlineExceeded) {
		t.Fatalf("socket ended with %v, want it still up at the deadline", end.err)
	}
}

// racingConn reproduces the interleaving socket.writerGrace exists for: the
// pump's Read fails FIRST, with a generic codeless error, while the
// heartbeat's Write is still inside Discord's close frame and has not
// reported anything yet.
//
// A real socket produces this whenever a writer takes the close frame: the
// writer's Close makes the parked Read return immediately, and the Write it
// was racing returns its own error microseconds later. The old non-blocking
// poll in firstError looked at that instant, found nothing, and reported no
// close code for a 4004.
type racingConn struct {
	mu      sync.Mutex
	reads   [][]byte
	writes  int
	release chan struct{}
	// lag is how long the failing Write takes to return after it has
	// released the pump's Read.
	lag  time.Duration
	once sync.Once
}

func newRacingConn(t *testing.T, lag time.Duration) *racingConn {
	t.Helper()
	hello, err := fastHello()
	if err != nil {
		t.Fatalf("marshal hello: %v", err)
	}
	return &racingConn{reads: [][]byte{hello}, release: make(chan struct{}), lag: lag}
}

func (c *racingConn) Read(ctx context.Context) ([]byte, error) {
	c.mu.Lock()
	if len(c.reads) > 0 {
		raw := c.reads[0]
		c.reads = c.reads[1:]
		c.mu.Unlock()
		return raw, nil
	}
	c.mu.Unlock()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-c.release:
		return nil, errors.New("use of closed network connection")
	}
}

func (c *racingConn) Write(context.Context, []byte) error {
	c.mu.Lock()
	c.writes++
	n := c.writes
	c.mu.Unlock()
	if n < 2 {
		return nil // the Identify goes through
	}
	c.unblockRead()
	time.Sleep(c.lag)
	return authFailure()
}

func (c *racingConn) Close() error {
	c.unblockRead()
	return nil
}

func (c *racingConn) Shutdown() error {
	c.unblockRead()
	return nil
}

func (c *racingConn) unblockRead() { c.once.Do(func() { close(c.release) }) }

func (c *racingConn) CloseCode(err error) int {
	if code := websocket.CloseStatus(err); code >= 0 {
		return int(code)
	}
	return 0
}

func (c *racingConn) CloseReason(err error) string {
	var ce websocket.CloseError
	if errors.As(err, &ce) {
		return ce.Reason
	}
	return ""
}

// The pump must wait for the writer rather than deciding from an empty
// channel that a socket died codeless. Without the grace this is a 4004
// reported as close code 0, which reconnects forever.
func TestCodelessReadWaitsForTheWritersCloseCode(t *testing.T) {
	end := oneSocketOver(t, newRacingConn(t, 50*time.Millisecond), 3*time.Second)
	code, err := end.code, end.err

	if code != ddiscord.CloseAuthenticationFailed {
		t.Fatalf("close code = %d, want %d: the read won the race and the write carried the frame",
			code, ddiscord.CloseAuthenticationFailed)
	}
	wantCloseFrame(t, err)
}

// And the wait is bounded. A writer stuck in a send on a connection nobody
// is draining is not about to produce a close code, and holding the
// reconnect behind it indefinitely trades one loop for a stall.
func TestCodelessReadGivesUpAfterTheGrace(t *testing.T) {
	conn := newRacingConn(t, writerGrace+time.Second)

	start := time.Now()
	code := oneSocketOver(t, conn, 3*time.Second).code

	if code != 0 {
		t.Fatalf("close code = %d, want 0: no writer reported inside the grace", code)
	}
	if waited := time.Since(start); waited > writerGrace+500*time.Millisecond {
		t.Fatalf("waited %s, want the grace to bound it at %s", waited, writerGrace)
	}
}
