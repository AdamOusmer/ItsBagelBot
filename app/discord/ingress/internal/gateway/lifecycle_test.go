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

// A pod that shuts down must say so. The status key has no TTL, so before
// this the dashboard showed a green pill for a bot whose process had exited
// -- forever, or until the next pod happened to write the key.
func TestShutdownReportsDownBeforeReturning(t *testing.T) {
	// No readErr and no closed channel: this socket just sits there, which
	// is what a healthy connection looks like when the pod is drained.
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
	// Shutdown is not a reconnect: nothing was scheduled, so nothing was
	// charged to the connect budget.
	if states := st.budgetStates(); len(states) != 0 {
		t.Fatalf("budget states = %+v, want none on shutdown", states)
	}
}

// The shutdown write must not inherit the cancelled context: a Valkey call
// under one fails instantly, which is the same as not writing at all.
func TestShutdownWriteGetsALiveContext(t *testing.T) {
	sess := Session{Status: &recStatus{}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// Inspected inside the callback: reportFinalDown cancels its own
	// context on the way out, so the only moment the write context is live
	// is while the write is happening.
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

// statusFunc is a Status that only cares about Down's context.
type statusFunc func(ctx context.Context)

func (f statusFunc) Up(context.Context, Up)           {}
func (f statusFunc) Down(ctx context.Context, _ Down) { f(ctx) }
func (f statusFunc) Event(context.Context)            {}
func (f statusFunc) Budget(context.Context, Budget)   {}

// A dial must not be able to park the whole ingress. Before dialTimeout the
// handshake inherited only Run's process-lifetime context, so a gateway host
// that accepted the TCP connection and then said nothing held the loop
// forever with a stale status key.
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

// The uptime that resets the backoff schedule is measured from READY, not
// from before the dial. A socket that never identified must report zero:
// counting a 30s hung handshake as uptime let a connection that carried no
// bytes reset the schedule and hammer Discord's identify budget.
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

// A second RESUMED on the same socket must not restamp the clock: it is
// still the same connection, and restamping would understate its uptime.
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

// A heartbeat ACK is the only traffic a bot in a silent guild sees, so it
// has to count as an event -- otherwise the liveness clock measures how busy
// the guild is instead of whether the socket is delivering.
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
