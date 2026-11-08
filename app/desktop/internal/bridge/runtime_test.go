// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package bridge

import (
	"context"
	"errors"
	"io"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"ItsBagelBot/app/desktop/internal/bridge/protocol"
	"ItsBagelBot/app/desktop/internal/bridge/session"
	"ItsBagelBot/app/desktop/internal/bridge/transport"
)

type runtimeTokenFunc func(context.Context) (string, error)

func (f runtimeTokenFunc) AccessToken(ctx context.Context) (string, error) { return f(ctx) }

type runtimeConsumerFunc func(*protocol.Event) error

func (f runtimeConsumerFunc) Apply(event *protocol.Event) error { return f(event) }

type runtimeControl struct {
	binds      chan BindRequest
	migrations chan MigrationRequest
}

func newRuntimeControl() *runtimeControl {
	return &runtimeControl{binds: make(chan BindRequest, 8), migrations: make(chan MigrationRequest, 4)}
}
func (c *runtimeControl) Bind(ctx context.Context, r BindRequest) error {
	select {
	case c.binds <- r:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (c *runtimeControl) RequestMigration(ctx context.Context, r MigrationRequest) error {
	select {
	case c.migrations <- r:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

type runtimeConn struct {
	frames chan *protocol.Message
	closed chan struct{}
	once   sync.Once
	reads  atomic.Int32
}

func newRuntimeConn() *runtimeConn {
	return &runtimeConn{frames: make(chan *protocol.Message, 64), closed: make(chan struct{})}
}
func (c *runtimeConn) Recv(ctx context.Context, dst *protocol.Message) error {
	c.reads.Add(1)
	select {
	case src := <-c.frames:
		defer src.ReturnToVTPool()
		raw := make([]byte, src.SizeVT())
		if _, err := src.MarshalToSizedBufferVT(raw); err != nil {
			return err
		}
		dst.ResetVT()
		return dst.UnmarshalVT(raw)
	case <-c.closed:
		return io.EOF
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (c *runtimeConn) Close() error {
	c.once.Do(func() {
		close(c.closed)
		for {
			select {
			case m := <-c.frames:
				m.ReturnToVTPool()
			default:
				return
			}
		}
	})
	return nil
}
func (c *runtimeConn) send(m *protocol.Message) {
	select {
	case c.frames <- m:
	case <-c.closed:
		m.ReturnToVTPool()
	}
}
func message(kind protocol.Kind) *protocol.Message {
	m := protocol.ServerMessageFromVTPool()
	m.Version = protocol.Version
	m.Kind = kind
	return m
}
func runtimeEvent(seq uint64) *protocol.Message {
	m := message(protocol.EventMessage)
	m.Event = protocol.EventFromVTPool()
	m.Event.Sequence = seq
	m.Event.Kind = protocol.EventKind_CHAT
	m.Event.Chat = protocol.ChatEventFromVTPool()
	m.Event.Chat.ChannelId = "channel"
	m.Event.Chat.UserId = "user"
	m.Event.Chat.DisplayName = "Bagel"
	m.Event.Chat.Text = "event-" + strconv.FormatUint(seq, 10)
	return m
}
func runtimeAwait(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.NewTimer(2 * time.Second)
	defer deadline.Stop()
	for !condition() {
		select {
		case <-deadline.C:
			t.Fatal("runtime condition timed out")
		default:
			time.Sleep(time.Millisecond)
		}
	}
}
func runtimeConfig(dial transport.Dialer, consumer Consumer) Config {
	return Config{Endpoint: "https://bridge.example", Control: newRuntimeControl(), Tokens: runtimeTokenFunc(func(context.Context) (string, error) { return "native-token", nil }), Consumer: consumer, Dial: dial, Session: session.Config{BackoffBase: time.Millisecond, BackoffMax: time.Millisecond, Jitter: func(time.Duration) time.Duration { return 0 }, MaxAttempts: 3}}
}
func startRuntime(t *testing.T, dial transport.Dialer, consumer Consumer) *Runtime {
	t.Helper()
	return startConfiguredRuntime(t, runtimeConfig(dial, consumer))
}
func startConfiguredRuntime(t *testing.T, config Config) *Runtime {
	t.Helper()
	r, _ := startRuntimeWithCancel(t, config)
	return r
}
func startRuntimeWithCancel(t *testing.T, config Config) (*Runtime, context.CancelFunc) {
	t.Helper()
	r, err := New(config)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	go r.Run(ctx)
	t.Cleanup(func() {
		cancel()
		wait, stop := context.WithTimeout(context.Background(), time.Second)
		defer stop()
		if err := r.Wait(wait); err != nil {
			t.Error(err)
		}
	})
	if err = r.Connect(t.Context()); err != nil {
		t.Fatal(err)
	}
	return r, cancel
}
func runtimeWelcome(t *testing.T, r *Runtime, c *runtimeConn, id string) {
	t.Helper()
	m := message(protocol.SessionStarted)
	m.SessionId = id
	m.BindWithinMs = 1000
	c.send(m)
	select {
	case req := <-r.config.Control.(*runtimeControl).binds:
		if req.Token != "native-token" || req.SessionID != id {
			t.Fatal("wrong native binding")
		}
	case <-time.After(time.Second):
		t.Fatal("bind missing after welcome")
	}
	m = message(protocol.SessionBound)
	m.SessionId = id
	c.send(m)
}
func runtimeAssertOpen(t *testing.T, c *runtimeConn) {
	t.Helper()
	select {
	case <-c.closed:
		t.Fatal("connection closed before replacement ready")
	default:
	}
}
func runtimeReconnect(c *runtimeConn, migration, target string) {
	m := message(protocol.Reconnect)
	m.Migration = migration
	m.WithinMs = 1000
	m.Target = target
	c.send(m)
}
func runtimeReady(c *runtimeConn, migration string, through uint64) {
	m := message(protocol.HandoffReady)
	m.Migration = migration
	m.Through = through
	c.send(m)
}

func TestRuntimeInitialBindingAndLease(t *testing.T) {
	conn := newRuntimeConn()
	entered := make(chan struct{})
	release := make(chan struct{})
	applied := make(chan string, 2)
	consumer := runtimeConsumerFunc(func(event *protocol.Event) error {
		if event.Sequence == 1 {
			close(entered)
			<-release
		}
		applied <- event.Chat.Text
		return nil
	})
	r := startRuntime(t, func(context.Context, transport.Request) (transport.Conn, error) { return conn, nil }, consumer)
	runtimeWelcome(t, r, conn, "first")
	runtimeAwait(t, func() bool { return r.Status().State == session.Online })
	conn.send(runtimeEvent(1))
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("consumer missing")
	}
	reads := conn.reads.Load()
	conn.send(runtimeEvent(2))
	time.Sleep(10 * time.Millisecond)
	if conn.reads.Load() != reads {
		t.Error("read receiver reused while consumer still holds lease")
	}
	close(release)
	for _, want := range []string{"event-1", "event-2"} {
		select {
		case got := <-applied:
			if got != want {
				t.Fatalf("payload changed: %s", got)
			}
		case <-time.After(time.Second):
			t.Fatal("event missing")
		}
	}
	runtimeAwait(t, func() bool { return r.Status().Cursor == 2 })
}
func TestRuntimeOverlappingHandoff(t *testing.T) {
	sockets := []*runtimeConn{newRuntimeConn(), newRuntimeConn(), newRuntimeConn()}
	var calls atomic.Int32
	delivered := make(chan uint64, 8)
	r := startRuntime(t, func(context.Context, transport.Request) (transport.Conn, error) { return sockets[calls.Add(1)-1], nil }, runtimeConsumerFunc(func(e *protocol.Event) error { delivered <- e.Sequence; return nil }))
	runtimeWelcome(t, r, sockets[0], "old")
	runtimeAwait(t, func() bool { return r.Status().State == session.Online })
	sockets[0].send(runtimeEvent(1))
	runtimeAwait(t, func() bool { return r.Status().Cursor == 1 })
	for i := 1; i <= 2; i++ {
		runtimeMove(t, r, sockets[i-1], sockets[i], i, calls.Load)
	}
	for _, want := range []uint64{1, 2, 3, 4, 5} {
		select {
		case got := <-delivered:
			if got != want {
				t.Fatalf("sequence got %d want %d", got, want)
			}
		case <-time.After(time.Second):
			t.Fatal("handoff lost event")
		}
	}
}
func runtimeMove(t *testing.T, r *Runtime, primary, pending *runtimeConn, step int, calls func() int32) {
	t.Helper()
	migration := "move" + strconv.Itoa(step)
	boundary := uint64(step * 2)
	runtimeReconnect(primary, migration, "")
	runtimeAwait(t, func() bool { return calls() == int32(step+1) })
	runtimeAssertOpen(t, primary)
	runtimeWelcome(t, r, pending, "candidate"+strconv.Itoa(step))
	runtimeAwait(t, func() bool { return r.Status().State == session.Migrating })
	pending.send(runtimeEvent(boundary + 1))
	runtimeReady(pending, migration, boundary)
	primary.send(runtimeEvent(boundary))
	runtimeAwait(t, func() bool { return r.Status().State == session.Online && r.Status().Cursor == boundary+1 })
	select {
	case <-primary.closed:
	default:
		t.Fatal("former primary still open")
	}
	runtimeAssertOpen(t, pending)
}
func TestRuntimeRetryBudgetAndLogout(t *testing.T) {
	var calls atomic.Int32
	r := startRuntime(t, func(context.Context, transport.Request) (transport.Conn, error) {
		calls.Add(1)
		return nil, errors.New("offline")
	}, runtimeConsumerFunc(func(*protocol.Event) error { return nil }))
	runtimeAwait(t, func() bool { return r.Status().State == session.Failed })
	if calls.Load() != 3 {
		t.Fatal("wrong attempt budget")
	}
	if err := r.Disconnect(t.Context()); err != nil {
		t.Fatal(err)
	}
	runtimeAwait(t, func() bool { return r.Status().State == session.Stopped })
	before := calls.Load()
	time.Sleep(10 * time.Millisecond)
	if before != calls.Load() {
		t.Fatal("logout left retries")
	}
}
func TestRuntimeStaleDialCompletionClosesOnlyStaleSocket(t *testing.T) {
	old, newer := newRuntimeConn(), newRuntimeConn()
	started := make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int32
	r := startRuntime(t, func(context.Context, transport.Request) (transport.Conn, error) {
		if calls.Add(1) == 1 {
			close(started)
			<-release
			return old, nil
		}
		return newer, nil
	}, runtimeConsumerFunc(func(*protocol.Event) error { return nil }))
	<-started
	if err := r.Disconnect(t.Context()); err != nil {
		t.Fatal(err)
	}
	runtimeAwait(t, func() bool { return r.Status().State == session.Stopped })
	if err := r.Connect(t.Context()); err != nil {
		t.Fatal(err)
	}
	runtimeAwait(t, func() bool { return calls.Load() == 2 })
	close(release)
	runtimeWelcome(t, r, newer, "current")
	runtimeAwait(t, func() bool { return r.Status().State == session.Online })
	runtimeAwait(t, func() bool {
		select {
		case <-old.closed:
			return true
		default:
			return false
		}
	})
	runtimeAssertOpen(t, newer)
}
func TestRuntimeNativeStreamRequest(t *testing.T) {
	conn := newRuntimeConn()
	requests := make(chan transport.Request, 2)
	r := startRuntime(t, func(_ context.Context, req transport.Request) (transport.Conn, error) {
		requests <- req
		return conn, nil
	}, runtimeConsumerFunc(func(*protocol.Event) error { return nil }))
	select {
	case req := <-requests:
		if req.Token != "native-token" || req.Endpoint != "https://bridge.example" {
			t.Fatal("wrong native stream request")
		}
	case <-time.After(time.Second):
		t.Fatal("stream missing")
	}
	if len(r.config.Control.(*runtimeControl).binds) != 0 {
		t.Fatal("bound before session_started")
	}
	runtimeWelcome(t, r, conn, "first")
	runtimeAwait(t, func() bool { return r.Status().State == session.Online })
	conn.send(message(protocol.Keepalive))
	conn.send(runtimeEvent(1))
	runtimeAwait(t, func() bool { return r.Status().Cursor == 1 })
	runtimeAssertOpen(t, conn)
}

type controlFailure struct{ err error }

func (c controlFailure) Bind(context.Context, BindRequest) error                  { return c.err }
func (c controlFailure) RequestMigration(context.Context, MigrationRequest) error { return c.err }
func TestRuntimeControlFailureClassification(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want session.State
	}{{"temporary", context.DeadlineExceeded, session.Failed}, {"auth", ErrAuthentication, session.AuthenticationRequired}} {
		t.Run(tc.name, func(t *testing.T) {
			conn := newRuntimeConn()
			config := runtimeConfig(func(context.Context, transport.Request) (transport.Conn, error) { return conn, nil }, runtimeConsumerFunc(func(*protocol.Event) error { return nil }))
			config.Control = controlFailure{tc.err}
			config.Session.MaxAttempts = 1
			r := startConfiguredRuntime(t, config)
			m := message(protocol.SessionStarted)
			m.SessionId = "first"
			m.BindWithinMs = 1000
			conn.send(m)
			runtimeAwait(t, func() bool { return r.Status().State == tc.want })
		})
	}
}

func preparedRuntime(t testing.TB) *Runtime {
	t.Helper()
	r := &Runtime{machine: session.New(session.Config{IdleTimeout: time.Hour}), config: Config{Consumer: runtimeConsumerFunc(func(*protocol.Event) error { return nil })}}
	r.timer = time.NewTimer(time.Hour)
	r.timer.Stop()
	t.Cleanup(func() { r.timer.Stop() })
	now := time.Now()
	r.machine.Handle(session.Event{Kind: session.Start}, now)
	id := r.machine.Snapshot().Primary
	r.machine.Handle(session.Event{Kind: session.Dialed, Connection: id}, now)
	r.machine.Handle(session.Event{Kind: session.Welcome, Connection: id, SessionID: "test", Within: time.Second}, now)
	r.machine.Handle(session.Event{Kind: session.Bound, Connection: id, SessionID: "test"}, now)
	r.workers[0] = worker{id: id, cancel: func() {}}
	return r
}
func BenchmarkRuntimeEventDispatch(b *testing.B) {
	r := preparedRuntime(b)
	id := r.machine.Snapshot().Primary
	m := runtimeEvent(1)
	defer m.ReturnToVTPool()
	ctx := context.Background()
	b.ReportAllocs()
	for b.Loop() {
		m.Event.Sequence = r.Status().Cursor + 1
		r.receive(ctx, notice{id: id, kind: frame, message: m})
	}
}

func TestCancellationKeepsConsumerLeaseUntilReturn(t *testing.T) {
	conn := newRuntimeConn()
	entered := make(chan struct{})
	release := make(chan struct{})
	intact := make(chan bool, 1)
	var releaseOnce sync.Once
	releaseConsumer := func() { releaseOnce.Do(func() { close(release) }) }
	config := runtimeConfig(func(context.Context, transport.Request) (transport.Conn, error) { return conn, nil }, runtimeConsumerFunc(func(event *protocol.Event) error {
		close(entered)
		<-release
		intact <- event.Sequence == 1 && event.Chat != nil && event.Chat.Text == "event-1"
		return nil
	}))
	r, cancel := startRuntimeWithCancel(t, config)
	t.Cleanup(releaseConsumer)
	runtimeWelcome(t, r, conn, "first")
	runtimeAwait(t, func() bool { return r.Status().State == session.Online })
	conn.send(runtimeEvent(1))
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("consumer missing")
	}
	cancel()
	wait, stop := context.WithTimeout(context.Background(), 20*time.Millisecond)
	err := r.Wait(wait)
	stop()
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("shutdown returned while consumer still owned its event")
	}
	releaseConsumer()
	select {
	case good := <-intact:
		if !good {
			t.Fatal("cancellation reset a consumer-owned event")
		}
	case <-time.After(time.Second):
		t.Fatal("consumer did not release")
	}
}

func TestPendingQueueSaturationRetainsPrimary(t *testing.T) {
	old, pending := newRuntimeConn(), newRuntimeConn()
	var calls atomic.Int32
	r := startRuntime(t, func(context.Context, transport.Request) (transport.Conn, error) {
		if calls.Add(1) == 1 {
			return old, nil
		}
		return pending, nil
	}, runtimeConsumerFunc(func(*protocol.Event) error { return nil }))
	runtimeWelcome(t, r, old, "primary")
	runtimeAwait(t, func() bool { return r.Status().State == session.Online })
	runtimeReconnect(old, "move", "")
	runtimeAwait(t, func() bool { return calls.Load() == 2 })
	runtimeWelcome(t, r, pending, "pending")
	for i := 1; i <= pendingCapacity+1; i++ {
		pending.send(runtimeEvent(uint64(i)))
	}
	runtimeAwait(t, func() bool { return r.Status().Problem == session.MigrationFailed })
	runtimeAssertOpen(t, old)
	old.send(runtimeEvent(1))
	runtimeAwait(t, func() bool { return r.Status().Cursor == 1 })
	if err := r.Disconnect(t.Context()); err != nil {
		t.Fatal(err)
	}
	runtimeAwait(t, func() bool { return r.Status().State == session.Stopped })
	if r.queue.count != 0 {
		t.Fatal("pooled candidate events survived teardown")
	}
}

func TestInvalidMigrationTargetKeepsPrimary(t *testing.T) {
	conn := newRuntimeConn()
	var calls atomic.Int32
	r := startRuntime(t, func(context.Context, transport.Request) (transport.Conn, error) { calls.Add(1); return conn, nil }, runtimeConsumerFunc(func(*protocol.Event) error { return nil }))
	runtimeWelcome(t, r, conn, "primary")
	runtimeAwait(t, func() bool { return r.Status().State == session.Online })
	runtimeReconnect(conn, "move", "https://untrusted.example")
	runtimeAwait(t, func() bool { return r.Status().Problem == session.MigrationFailed })
	runtimeAssertOpen(t, conn)
	if calls.Load() != 1 {
		t.Fatal("untrusted migration target was dialed")
	}
	conn.send(runtimeEvent(1))
	runtimeAwait(t, func() bool { return r.Status().Cursor == 1 })
}

func TestMalformedTypedEventNeverReachesConsumer(t *testing.T) {
	conn := newRuntimeConn()
	var delivered atomic.Int32
	config := runtimeConfig(func(context.Context, transport.Request) (transport.Conn, error) { return conn, nil }, runtimeConsumerFunc(func(*protocol.Event) error { delivered.Add(1); return nil }))
	config.Session.MaxAttempts = 1
	r := startConfiguredRuntime(t, config)
	runtimeWelcome(t, r, conn, "primary")
	runtimeAwait(t, func() bool { return r.Status().State == session.Online })
	malformed := runtimeEvent(1)
	malformed.Event.Kind = protocol.EventKind_EVENT_UNSPECIFIED
	conn.send(malformed)
	runtimeAwait(t, func() bool { return r.Status().State == session.Failed })
	if delivered.Load() != 0 {
		t.Fatal("malformed typed event reached consumer")
	}
}

func TestPendingQueueOwnershipAndWrap(t *testing.T) {
	var queue pendingFrames
	for cycle := 0; cycle < 3; cycle++ {
		for i := 0; i < pendingCapacity; i++ {
			m := runtimeEvent(uint64(i + 1))
			event := m.Event
			m.Event = nil
			m.ReturnToVTPool()
			if !queue.push(1, event) {
				event.ReturnToVTPool()
				t.Fatal("queue push failed")
			}
		}
		if queue.push(1, nil) {
			t.Fatal("nil event accepted")
		}
		for i := 0; i < pendingCapacity; i++ {
			event := queue.pop()
			if event.Sequence != uint64(i+1) {
				t.Fatal("queue reordered events")
			}
			event.ReturnToVTPool()
		}
	}
	m := runtimeEvent(1)
	event := m.Event
	m.Event = nil
	m.ReturnToVTPool()
	queue.push(1, event)
	queue.reset()
	if queue.count != 0 || queue.id != 0 {
		t.Fatal("queue reset retained owner")
	}
}

func TestShutdownAcknowledgesQueuedCandidateLease(t *testing.T) {
	old, pending := newRuntimeConn(), newRuntimeConn()
	var calls atomic.Int32
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	config := runtimeConfig(func(context.Context, transport.Request) (transport.Conn, error) {
		if calls.Add(1) == 1 {
			return old, nil
		}
		return pending, nil
	}, runtimeConsumerFunc(func(*protocol.Event) error { close(entered); <-release; return nil }))
	r, cancel := startRuntimeWithCancel(t, config)
	t.Cleanup(unblock)
	runtimeWelcome(t, r, old, "primary")
	runtimeAwait(t, func() bool { return r.Status().State == session.Online })
	runtimeReconnect(old, "move", "")
	runtimeAwait(t, func() bool { return calls.Load() == 2 })
	runtimeWelcome(t, r, pending, "pending")
	old.send(runtimeEvent(1))
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("primary consumer missing")
	}
	pending.send(runtimeEvent(2))
	runtimeAwait(t, func() bool { return len(r.incoming) > 0 })
	cancel()
	unblock()
	wait, stop := context.WithTimeout(context.Background(), time.Second)
	defer stop()
	if err := r.Wait(wait); err != nil {
		t.Fatal("shutdown did not release queued stream lease:", err)
	}
}

func TestConsumerMutationCannotMoveAppliedCursor(t *testing.T) {
	r := preparedRuntime(t)
	r.config.Consumer = runtimeConsumerFunc(func(event *protocol.Event) error { event.Sequence = 999; return nil })
	m := runtimeEvent(1)
	defer m.ReturnToVTPool()
	r.receive(context.Background(), notice{id: r.machine.Snapshot().Primary, kind: frame, message: m})
	if r.Status().Cursor != 1 {
		t.Fatal("consumer corrupted applied cursor")
	}
}
