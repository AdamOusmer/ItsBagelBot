// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package gateway

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	ddiscord "ItsBagelBot/internal/domain/discord"
	"ItsBagelBot/pkg/codec"

	"github.com/coder/websocket"
)

var (
	errClosed  = errors.New("websocket closed")
	errRefused = errors.New("dial refused")
)

type scriptedConn struct {
	mu            sync.Mutex
	reads         [][]byte
	wrote         [][]byte
	readErr       error
	closeCode     int
	closeReason   string
	writeErr      error
	writeErrAfter int
	closed        chan struct{}
	closeOnce     sync.Once
	closeSent     []websocket.StatusCode
}

func (s *scriptedConn) Read(ctx context.Context) ([]byte, error) {
	s.mu.Lock()
	if len(s.reads) == 0 {
		err := s.readErr
		closed := s.closed
		s.mu.Unlock()
		if err != nil {
			return nil, err
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-closed:
			return nil, errors.New("use of closed network connection")
		}
	}
	raw := s.reads[0]
	s.reads = s.reads[1:]
	s.mu.Unlock()
	return raw, nil
}

func (s *scriptedConn) Write(_ context.Context, data []byte) error {
	cp := append([]byte(nil), data...)
	s.mu.Lock()
	s.wrote = append(s.wrote, cp)
	var err error
	if len(s.wrote) > s.writeErrAfter {
		err = s.writeErr
	}
	s.mu.Unlock()
	return err
}

func (s *scriptedConn) Close() error { return s.closeWith(reconnectingClose) }

func (s *scriptedConn) Shutdown() error { return s.closeWith(websocket.StatusNormalClosure) }

func (s *scriptedConn) closeWith(code websocket.StatusCode) error {
	s.mu.Lock()
	closed := s.closed
	s.closeSent = append(s.closeSent, code)
	s.mu.Unlock()
	if closed != nil {
		s.closeOnce.Do(func() { close(closed) })
	}
	return nil
}

func (s *scriptedConn) closeCodes() []websocket.StatusCode {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]websocket.StatusCode(nil), s.closeSent...)
}

func (s *scriptedConn) CloseCode(err error) int {
	if code := websocket.CloseStatus(err); code >= 0 {
		return int(code)
	}
	return s.closeCode
}

func (s *scriptedConn) CloseReason(err error) string {
	var ce websocket.CloseError
	if errors.As(err, &ce) {
		return ce.Reason
	}
	return s.closeReason
}

func (s *scriptedConn) wroteSnapshot() [][]byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([][]byte(nil), s.wrote...)
}

type script struct {
	reads       [][]byte
	readErr     error
	closeCode   int
	closeReason string
	writeErr    error
	dialErr     error
}

func (s script) conn() *scriptedConn {
	return &scriptedConn{
		reads:         s.reads,
		readErr:       s.readErr,
		closeCode:     s.closeCode,
		closeReason:   s.closeReason,
		writeErr:      s.writeErr,
		writeErrAfter: 1,
		closed:        make(chan struct{}),
	}
}

type dialer struct {
	mu        sync.Mutex
	scripts   []script
	urls      []string
	conns     []*scriptedConn
	unbounded int
}

func (d *dialer) dial(ctx context.Context, url string) (Conn, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.urls = append(d.urls, url)
	if !boundedBy(ctx, dialTimeout) {
		d.unbounded++
	}
	s := d.scripts[min(len(d.urls), len(d.scripts))-1]
	if s.dialErr != nil {
		return nil, s.dialErr
	}
	conn := s.conn()
	d.conns = append(d.conns, conn)
	return conn, nil
}

func (d *dialer) opened(t *testing.T) []int {
	d.mu.Lock()
	defer d.mu.Unlock()
	var ops []int
	for _, c := range d.conns {
		wrote := opsWritten(t, c.wroteSnapshot())
		ops = append(ops, wrote[:min(len(wrote), 1)]...)
	}
	return ops
}

func (d *dialer) wrote(i int) [][]byte {
	d.mu.Lock()
	defer d.mu.Unlock()
	if i >= len(d.conns) {
		return nil
	}
	return d.conns[i].wroteSnapshot()
}

func boundedBy(ctx context.Context, limit time.Duration) bool {
	if ctx.Err() != nil {
		return false
	}
	deadline, ok := ctx.Deadline()
	return ok && time.Until(deadline) <= limit
}

type recHandler struct {
	err    error
	idents []Identity
	types  []string
}

func (r *recHandler) Ready(_ context.Context, ident Identity) error {
	r.idents = append(r.idents, ident)
	return nil
}

func (r *recHandler) Dispatch(_ context.Context, ev Event) error {
	r.types = append(r.types, ev.Type)
	return r.err
}

type budgetSeen struct {
	Connects  int
	Flapping  bool
	AtCeiling bool
	Parked    bool
}

type recStatus struct {
	mu        sync.Mutex
	stopAfter int
	stop      context.CancelFunc
	ups       []Up
	downs     []Down
	finals    int
	budgets   []budgetSeen
	events    int
}

func (r *recStatus) Up(_ context.Context, up Up) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ups = append(r.ups, up)
}

func (r *recStatus) Down(ctx context.Context, d Down) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.downs = append(r.downs, d)
	if boundedBy(ctx, finalDownTimeout) {
		r.finals++
	}
}

func (r *recStatus) Event(context.Context) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events++
}

func (r *recStatus) Budget(_ context.Context, b Budget) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.budgets = append(r.budgets, budgetSeen{
		Connects: b.Connects, Flapping: b.Flapping, AtCeiling: b.AtCeiling, Parked: !b.ParkUntil.IsZero(),
	})
	if len(r.budgets) == r.stopAfter {
		r.stop()
	}
}

type fakePresence struct {
	mu        sync.Mutex
	sends     int
	refreshes int
	forgets   int
}

func (f *fakePresence) Refresh(context.Context) (string, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.refreshes++
	if f.refreshes > f.sends {
		return "", false
	}
	return fmt.Sprintf("watch-%d streams", f.refreshes), true
}

func (f *fakePresence) Forget() {
	f.mu.Lock()
	f.forgets++
	f.mu.Unlock()
}

func (f *fakePresence) counts() (refreshes, forgets int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.refreshes, f.forgets
}

type fakeConnectLog struct {
	mu   sync.Mutex
	seen []time.Time
	fail error
}

func (f *fakeConnectLog) Load(_ context.Context, since time.Time) ([]time.Time, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail != nil {
		return nil, f.fail
	}
	kept := make([]time.Time, 0, len(f.seen))
	for _, at := range f.seen {
		if !at.Before(since) {
			kept = append(kept, at)
		}
	}
	f.seen = kept
	return append([]time.Time(nil), kept...), nil
}

func (f *fakeConnectLog) forget() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.seen = nil
}

func (f *fakeConnectLog) Add(_ context.Context, at time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail != nil {
		return f.fail
	}
	f.seen = append(f.seen, at)
	return nil
}

type budgetClock struct{ t time.Time }

func (c *budgetClock) now() time.Time { return c.t }

func (c *budgetClock) advance(d time.Duration) { c.t = c.t.Add(d) }

func fastBudget(ceiling int, store ConnectLog) *connectBudget {
	return &connectBudget{
		sched: budgetSchedule{
			minInterval: time.Millisecond,
			flapUptime:  time.Nanosecond,
			flapStreak:  flapStreak,
			flapWait:    flapWait,
			ceiling:     ceiling,
			window:      connectWindow,
		},
		now:   time.Now,
		store: store,
	}
}

func mustRaw(t *testing.T, v any) []byte {
	t.Helper()
	raw, err := codec.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func intPtr(v int) *int { return &v }

func helloFrame(t *testing.T) []byte {
	t.Helper()
	raw, err := codec.Marshal(packet{Op: opHello, D: mustRaw(t, helloData{HeartbeatInterval: 50000})})
	if err != nil {
		t.Fatalf("marshal hello: %v", err)
	}
	return raw
}

func fastHello() ([]byte, error) {
	d, err := codec.Marshal(helloData{HeartbeatInterval: 10})
	if err != nil {
		return nil, err
	}
	return codec.Marshal(packet{Op: opHello, D: d})
}

func frame(t *testing.T, pkt packet) []byte {
	t.Helper()
	return mustRaw(t, pkt)
}

func dispatchPacket(t *testing.T, name string, data any) []byte {
	t.Helper()
	return frame(t, packet{Op: opDispatch, T: name, D: mustRaw(t, data)})
}

func opsWritten(t *testing.T, frames [][]byte) []int {
	t.Helper()
	var ops []int
	for _, raw := range frames {
		var pkt packet
		if err := codec.Unmarshal(raw, &pkt); err != nil {
			continue
		}
		ops = append(ops, pkt.Op)
	}
	return ops
}

func firstOpIs(ops []int, want int) bool {
	return len(ops) > 0 && ops[0] == want
}

func authFailure() error {
	return websocket.CloseError{
		Code:   websocket.StatusCode(ddiscord.CloseAuthenticationFailed),
		Reason: "Authentication failed.",
	}
}
