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

func TestWSConnCloseCodeReadsRealFrames(t *testing.T) {
	var w wsConn
	cases := []struct {
		name string
		err  error
		want int
	}{
		{
			name: "close frame",
			err:  websocket.CloseError{Code: websocket.StatusCode(ddiscord.CloseAuthenticationFailed), Reason: "Authentication failed."},
			want: ddiscord.CloseAuthenticationFailed,
		},
		{
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
	if got := w.CloseCode(websocket.CloseError{Code: websocket.StatusNormalClosure}); got != int(websocket.StatusNormalClosure) {
		t.Fatalf("CloseCode(normal closure) = %d, want %d", got, websocket.StatusNormalClosure)
	}
}

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

func authFailure() error {
	return websocket.CloseError{
		Code:   websocket.StatusCode(ddiscord.CloseAuthenticationFailed),
		Reason: "Authentication failed.",
	}
}

func heartbeatFailingConn(t *testing.T) *scriptedConn {
	t.Helper()
	hello, err := fastHello()
	if err != nil {
		t.Fatalf("marshal hello: %v", err)
	}
	return &scriptedConn{
		reads:         [][]byte{hello},
		writeErr:      authFailure(),
		writeErrAfter: 1,
		closed:        make(chan struct{}),
	}
}

func oneSocketOver(t *testing.T, conn Conn, timeout time.Duration) sessionEnd {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	sess := Session{Token: "t", Dial: func(context.Context, string) (Conn, error) { return conn, nil }}
	return sess.oneSocket(ctx, "ws://x", &resumeState{})
}

func wantCloseFrame(t *testing.T, err error) {
	t.Helper()
	if !errors.As(err, &websocket.CloseError{}) {
		t.Fatalf("err = %v, want the close frame, not the pump's generic read error", err)
	}
}

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

func fastHello() ([]byte, error) {
	d, err := codec.Marshal(helloData{HeartbeatInterval: 10})
	if err != nil {
		return nil, err
	}
	return codec.Marshal(packet{Op: opHello, D: d})
}

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

func TestHeartbeatEndsASocketThatStopsAcking(t *testing.T) {
	end := oneSocketOver(t, ackingConn(t, 0), 2*time.Second)

	if !errors.Is(end.err, errZombie) {
		t.Fatalf("socket ended with %v, want the unacknowledged-heartbeat error", end.err)
	}
}

func TestHeartbeatKeepsASocketThatAcks(t *testing.T) {
	end := oneSocketOver(t, ackingConn(t, 50), 200*time.Millisecond)

	if !errors.Is(end.err, context.DeadlineExceeded) {
		t.Fatalf("socket ended with %v, want it still up at the deadline", end.err)
	}
}

type racingConn struct {
	mu      sync.Mutex
	reads   [][]byte
	writes  int
	release chan struct{}
	lag     time.Duration
	once    sync.Once
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
		return nil
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

func TestCodelessReadWaitsForTheWritersCloseCode(t *testing.T) {
	end := oneSocketOver(t, newRacingConn(t, 50*time.Millisecond), 3*time.Second)
	code, err := end.code, end.err

	if code != ddiscord.CloseAuthenticationFailed {
		t.Fatalf("close code = %d, want %d: the read won the race and the write carried the frame",
			code, ddiscord.CloseAuthenticationFailed)
	}
	wantCloseFrame(t, err)
}

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
