// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package gateway

import (
	"context"
	"errors"
	"testing"
	"time"

	"ItsBagelBot/pkg/codec"
)

func TestShutdownReportsDownBeforeReturning(t *testing.T) {
	conn := &scriptedConn{reads: [][]byte{helloFrame(t)}}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Millisecond)
	defer cancel()
	st := &recStatus{}
	sess := Session{Token: "t", Dial: func(context.Context, string) (Conn, error) { return conn, nil }, Status: st}

	if err := sess.Run(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Run err = %v, want the context error", err)
	}
	downs := st.downs()
	if len(downs) != 1 {
		t.Fatalf("Down calls = %d, want exactly one for the shutdown transition", len(downs))
	}
	if downs[0].Fatal {
		t.Fatalf("shutdown Down = %+v, want it not marked fatal", downs[0])
	}
	if states := st.budgetStates(); len(states) != 0 {
		t.Fatalf("budget states = %+v, want none on shutdown", states)
	}
}

func TestShutdownWriteGetsALiveContext(t *testing.T) {
	sess := Session{Status: &recStatus{}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	var called bool
	var liveErr error
	var deadline time.Time
	var hadDeadline bool
	sess.Status = statusFunc(func(c context.Context) {
		called = true
		liveErr = c.Err()
		deadline, hadDeadline = c.Deadline()
	})
	sess.reportFinalDown(ctx, 4000, errors.New("draining"))

	if !called {
		t.Fatal("Down was never called")
	}
	if liveErr != nil {
		t.Fatalf("shutdown write context was already dead: %v", liveErr)
	}
	if !hadDeadline || time.Until(deadline) > finalDownTimeout {
		t.Fatalf("shutdown write deadline = %v (ok=%t), want at most %s", deadline, hadDeadline, finalDownTimeout)
	}
}

type statusFunc func(ctx context.Context)

func (f statusFunc) Up(context.Context, Up)           {}
func (f statusFunc) Down(ctx context.Context, _ Down) { f(ctx) }
func (f statusFunc) Event(context.Context)            {}
func (f statusFunc) Budget(context.Context, Budget)   {}

func TestDialIsBounded(t *testing.T) {
	var deadline time.Time
	var had bool
	dial := func(ctx context.Context, _ string) (Conn, error) {
		deadline, had = ctx.Deadline()
		return nil, errors.New("refused")
	}
	sess := Session{Token: "t", Dial: dial}

	end := sess.oneSocket(context.Background(), "ws://x", &resumeState{})

	if end.err == nil {
		t.Fatal("oneSocket must surface the dial error")
	}
	if !had {
		t.Fatal("Dial got a context with no deadline; a blackholed handshake would hang forever")
	}
	if d := time.Until(deadline); d <= 0 || d > dialTimeout {
		t.Fatalf("dial deadline is %s away, want (0, %s]", d, dialTimeout)
	}
}

func TestUptimeIsMeasuredFromReady(t *testing.T) {
	dead := &scriptedConn{readErr: errors.New("connection reset")}
	sess := Session{Token: "t", Dial: func(context.Context, string) (Conn, error) { return dead, nil }}
	ctx := context.Background()

	up := sess.connect(ctx, "ws://x", &resumeState{}).up
	if up != 0 {
		t.Fatalf("uptime of a socket that never reached READY = %s, want 0", up)
	}

	ready := readyFrame(t)
	live := &scriptedConn{reads: [][]byte{helloFrame(t), ready}, readErr: errors.New("connection reset")}
	sess.Dial = func(context.Context, string) (Conn, error) { return live, nil }
	up = sess.connect(ctx, "ws://x", &resumeState{}).up
	if up <= 0 {
		t.Fatalf("uptime after READY = %s, want a positive duration", up)
	}
}

func TestResumeStateUptimeStampsOnce(t *testing.T) {
	st := &resumeState{}
	base := time.Unix(1_700_000_000, 0)
	if got := st.upFor(base); got != 0 {
		t.Fatalf("uptime before READY = %s, want 0", got)
	}
	st.markUp(base, false)
	st.markUp(base.Add(time.Minute), false)
	if got := st.upFor(base.Add(2 * time.Minute)); got != 2*time.Minute {
		t.Fatalf("uptime = %s, want 2m measured from the first READY", got)
	}
	st.resetUp()
	if got := st.upFor(base.Add(3 * time.Minute)); got != 0 {
		t.Fatalf("uptime after a new attempt started = %s, want 0", got)
	}
}

func TestHeartbeatAckCountsAsAnEvent(t *testing.T) {
	ack, err := codec.Marshal(packet{Op: opHeartbeatAck})
	if err != nil {
		t.Fatal(err)
	}
	conn := &scriptedConn{reads: [][]byte{helloFrame(t), ack}, readErr: errors.New("done")}
	st := &recStatus{}
	sess := Session{Token: "t", Dial: func(context.Context, string) (Conn, error) { return conn, nil }, Status: st}

	_ = sess.oneSocket(context.Background(), "ws://x", &resumeState{})

	if got := st.events(); got != 1 {
		t.Fatalf("events = %d, want 1: the ACK must advance the liveness clock", got)
	}
}

func readyFrame(t *testing.T) []byte {
	t.Helper()
	raw, err := codec.Marshal(packet{
		Op: opDispatch, T: eventReady, S: intPtr(1),
		D: mustRaw(t, readyData{SessionID: "sess-1", ResumeGatewayURL: "ws://resume"}),
	})
	if err != nil {
		t.Fatalf("marshal ready: %v", err)
	}
	return raw
}
