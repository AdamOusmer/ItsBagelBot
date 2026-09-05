// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package gateway

import (
	"context"
	"errors"
	"fmt"
	"net"
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

// TestWriteCloseCodeReachesTheFatalPath is the regression test for the
// token reset: the code must come back off a write error, not off the
// generic "closed connection" the pump reads afterwards.
func TestWriteCloseCodeReachesTheFatalPath(t *testing.T) {
	conn := heartbeatFailingConn(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	sess := Session{Token: "t", Dial: func(context.Context, string) (Conn, error) { return conn, nil }}

	code, err := sess.oneSocket(ctx, "ws://x", &resumeState{})

	if code != ddiscord.CloseAuthenticationFailed {
		t.Fatalf("close code = %d, want %d off the write error", code, ddiscord.CloseAuthenticationFailed)
	}
	if !ddiscord.FatalCloseCode(code) {
		t.Fatalf("code %d must be fatal", code)
	}
	if !errors.As(err, &websocket.CloseError{}) {
		t.Fatalf("err = %v, want the close frame, not the pump's generic read error", err)
	}
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
	downs := st.downs()
	if len(downs) != 1 || !downs[0].Fatal || downs[0].Code != ddiscord.CloseAuthenticationFailed {
		t.Fatalf("Down = %+v, want one fatal 4004", downs)
	}
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
