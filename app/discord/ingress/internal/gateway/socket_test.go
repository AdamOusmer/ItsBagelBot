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

	"github.com/coder/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func wsURL(srv *httptest.Server) string { return "ws" + strings.TrimPrefix(srv.URL, "http") }

func echoOnce(ctx context.Context, c *websocket.Conn) {
	typ, data, err := c.Read(ctx)
	if err == nil {
		_ = c.Write(ctx, typ, data)
	}
}

func dialEcho(t *testing.T) (Conn, <-chan websocket.StatusCode) {
	t.Helper()
	peerClose := make(chan websocket.StatusCode, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		echoOnce(r.Context(), c)
		_, _, rerr := c.Read(r.Context())
		peerClose <- websocket.CloseStatus(rerr)
	}))
	t.Cleanup(srv.Close)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	conn, err := DialWS(ctx, wsURL(srv))
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Shutdown() })
	return conn, peerClose
}

func TestDialWSRoundTripsFramesAndShutsDownNormally(t *testing.T) {
	conn, peerClose := dialEcho(t)
	ctx := context.Background()

	require.NoError(t, conn.Write(ctx, []byte(`{"op":1}`)))
	got, err := conn.Read(ctx)
	require.NoError(t, err)
	require.NoError(t, conn.Shutdown())

	assert.Equal(t, `{"op":1}`, string(got))
	assert.Equal(t, websocket.StatusNormalClosure, <-peerClose)
}

func TestDialWSRejectsAPeerThatDoesNotUpgrade(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()

	_, err := DialWS(context.Background(), wsURL(srv))

	require.Error(t, err)
}

type closeSeen struct {
	Code   int
	Reason string
}

func TestDialWSConnReadsTheCloseFrame(t *testing.T) {
	conn, _ := dialEcho(t)
	cases := []struct {
		name string
		err  error
		want closeSeen
	}{
		{
			name: "close frame",
			err:  websocket.CloseError{Code: websocket.StatusCode(ddiscord.CloseAuthenticationFailed), Reason: "Authentication failed."},
			want: closeSeen{Code: ddiscord.CloseAuthenticationFailed, Reason: "Authentication failed."},
		},
		{
			name: "wrapped close frame",
			err:  fmt.Errorf("read packet: %w", websocket.CloseError{Code: websocket.StatusCode(ddiscord.CloseDisallowedIntents)}),
			want: closeSeen{Code: ddiscord.CloseDisallowedIntents},
		},
		{
			name: "normal closure",
			err:  websocket.CloseError{Code: websocket.StatusNormalClosure},
			want: closeSeen{Code: int(websocket.StatusNormalClosure)},
		},
		{name: "plain network error", err: &net.OpError{Op: "read", Err: errors.New("connection reset by peer")}},
		{name: "context cancellation", err: context.Canceled},
		{name: "nil"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, closeSeen{Code: conn.CloseCode(tc.err), Reason: conn.CloseReason(tc.err)})
		})
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
