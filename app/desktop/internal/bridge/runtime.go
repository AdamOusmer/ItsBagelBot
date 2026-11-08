// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Package bridge adapts the app-owned session supervisor to transport workers.
// Credentials and event consumers are native Go ports, never frontend inputs.
package bridge

import (
	"context"
	"errors"
	"net/url"
	"sync"
	"time"

	"ItsBagelBot/app/desktop/internal/bridge/protocol"
	"ItsBagelBot/app/desktop/internal/bridge/session"
	"ItsBagelBot/app/desktop/internal/bridge/transport"
)

var ErrConfiguration = errors.New("bridge: endpoint, native token provider and consumer and native stream control required")
var ErrStopped = errors.New("bridge: runtime stopped")

// ErrAuthentication asks the supervisor to stop retries and require sign-in.
var ErrAuthentication = errors.New("bridge: authentication required")

// TokenProvider owns refresh serialization. ErrAuthentication requests native sign-in; other errors remain retryable.
// Tokens must never belong to a different active account.
type TokenProvider interface {
	AccessToken(context.Context) (string, error)
}

// Consumer uses a typed event lease: it must finish using the event and all its
// fields before returning. Events are read-only: no retention, mutation, reset or
// pool return. Failure must not apply side effects or advance state.
type Consumer interface {
	Apply(*protocol.Event) error
}
type BindRequest = transport.BindRequest
type MigrationRequest = transport.MigrationRequest

// Control performs typed native HTTPS account operations. The WebSocket event
// path remains receive-only. Calls honor their supplied deadlines.
type Control interface {
	Bind(context.Context, BindRequest) error
	RequestMigration(context.Context, MigrationRequest) error
}

type Config struct {
	Control  Control
	Endpoint string
	Tokens   TokenProvider
	Consumer Consumer
	Dial     transport.Dialer
	Session  session.Config
}

type notice struct {
	id      session.Connection
	kind    uint8
	conn    transport.Conn
	message *protocol.Message
	ack     chan struct{}
}

const (
	connected uint8 = iota
	frame
	broken
	unauthorized
	bindFailed
	migrationRequestFailed
)

type worker struct {
	id        session.Connection
	conn      transport.Conn
	cancel    context.CancelFunc
	controls  chan session.Effect
	sessionID string
}

type Runtime struct {
	workersWG sync.WaitGroup
	config    Config
	machine   *session.Machine
	commands  chan session.Kind
	incoming  chan notice
	done      chan struct{}
	mu        sync.RWMutex
	view      session.View
	workers   [2]worker
	timer     *time.Timer
	timerC    <-chan time.Time
	target    string
	queue     pendingFrames
}

func New(config Config) (*Runtime, error) {
	if !validAuthority(config.Endpoint) || config.Tokens == nil || config.Consumer == nil || config.Dial == nil || config.Control == nil {
		return nil, ErrConfiguration
	}
	r := &Runtime{config: config, machine: session.New(config.Session), commands: make(chan session.Kind, 4), incoming: make(chan notice, 4), done: make(chan struct{}), target: config.Endpoint}
	r.timer = time.NewTimer(time.Hour)
	r.timer.Stop()
	r.view = r.machine.Snapshot()
	return r, nil
}

func (r *Runtime) Status() session.View { r.mu.RLock(); defer r.mu.RUnlock(); return r.view }
func (r *Runtime) command(ctx context.Context, k session.Kind) error {
	select {
	case <-r.done:
		return ErrStopped
	default:
	}
	select {
	case r.commands <- k:
		return nil
	case <-r.done:
		return ErrStopped
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (r *Runtime) Connect(ctx context.Context) error    { return r.command(ctx, session.Start) }
func (r *Runtime) Retry(ctx context.Context) error      { return r.command(ctx, session.Retry) }
func (r *Runtime) Disconnect(ctx context.Context) error { return r.command(ctx, session.Stop) }

// Run has one owner and is called exactly once by the Wails service lifecycle.
func (r *Runtime) Run(ctx context.Context) {
	defer close(r.done)
	defer r.timer.Stop()
	defer r.shutdownWorkers()
	for {
		select {
		case <-ctx.Done():
			r.handle(ctx, session.Event{Kind: session.Stop})
			return
		case command := <-r.commands:
			r.handle(ctx, session.Event{Kind: command})
		case <-r.timerC:
			r.timerC = nil
			r.handle(ctx, session.Event{Kind: session.Tick})
		case n := <-r.incoming:
			r.receive(ctx, n)
		}
	}
}
func (r *Runtime) Wait(ctx context.Context) error {
	select {
	case <-r.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (r *Runtime) publish() { r.mu.Lock(); r.view = r.machine.Snapshot(); r.mu.Unlock() }
func (r *Runtime) handle(ctx context.Context, event session.Event) {
	effects := r.machine.Handle(event, time.Now())
	for i := 0; i < effects.Count; i++ {
		if !r.effect(ctx, effects.Items[i]) {
			r.fail(ctx, effects.Items[i].Connection)
			return
		}
	}
	r.publish()
	r.flush(ctx)
}
func (r *Runtime) effect(ctx context.Context, e session.Effect) bool {
	switch e.Action {
	case session.Dial:
		return r.dial(ctx, e.Connection)
	case session.Bind, session.RequestMigration:
		if w := r.find(e.Connection); w != nil {
			if e.Action == session.Bind {
				w.sessionID = e.SessionID
			} else {
				e.SessionID = w.sessionID
			}
			select {
			case w.controls <- e:
			default:
				return false
			}
		}
	case session.Close:
		r.closeWorker(e.Connection)
	case session.ArmTimer:
		r.timer.Stop()
		r.timerC = nil
		if !e.At.IsZero() {
			r.timer.Reset(time.Until(e.At))
			r.timerC = r.timer.C
		}
	}
	return true
}
func (r *Runtime) find(id session.Connection) *worker {
	for i := range r.workers {
		if r.workers[i].id == id && id != 0 {
			return &r.workers[i]
		}
	}
	return nil
}
func (r *Runtime) closeWorker(id session.Connection) {
	if w := r.find(id); w != nil {
		w.cancel()
		if w.conn != nil {
			_ = w.conn.Close()
		}
		*w = worker{}
	}
	if r.queue.id == id {
		r.queue.reset()
	}
}
func (r *Runtime) closeWorkers() {
	for i := range r.workers {
		r.closeWorker(r.workers[i].id)
	}
}
func (r *Runtime) fail(ctx context.Context, id session.Connection) {
	r.handle(ctx, session.Event{Kind: session.Broken, Connection: id})
}
func (r *Runtime) post(ctx context.Context, n notice) bool {
	select {
	case r.incoming <- n:
		return true
	case <-ctx.Done():
		return false
	case <-r.done:
		return false
	}
}
func (r *Runtime) dial(ctx context.Context, id session.Connection) bool {
	for i := range r.workers {
		if r.workers[i].id != 0 {
			continue
		}
		child, cancel := context.WithCancel(ctx)
		controls := make(chan session.Effect, 1)
		r.workers[i] = worker{id: id, cancel: cancel, controls: controls}
		view := r.machine.Snapshot()
		request := transport.Request{Endpoint: r.config.Endpoint}
		if view.Pending == id {
			request.Endpoint = r.target
		}
		r.workersWG.Add(1)
		go r.connectWorker(child, id, request, controls)
		return true
	}
	return false
}
func (r *Runtime) connectWorker(ctx context.Context, id session.Connection, request transport.Request, controls <-chan session.Effect) {
	defer r.workersWG.Done()
	token, err := r.nativeToken(ctx)
	if err != nil {
		r.reportError(ctx, id, err)
		return
	}
	request.Token = token
	conn, err := r.config.Dial(ctx, request)
	if err != nil {
		r.reportError(ctx, id, err)
		return
	}
	defer conn.Close()
	if !r.post(ctx, notice{id: id, kind: connected, conn: conn}) {
		return
	}
	r.workersWG.Add(1)
	go r.controller(ctx, id, controls)

	message := protocol.ServerMessageFromVTPool()
	defer message.ReturnToVTPool()
	ack := make(chan struct{}, 1)
	for {
		if err := conn.Recv(ctx, message); err != nil {
			r.reportError(ctx, id, err)
			return
		}
		if !r.post(ctx, notice{id: id, kind: frame, message: message, ack: ack}) {
			return
		}
		// Once published, cancellation cannot revoke the actor's message lease.
		// Shutdown drains notices to acknowledge every outstanding owned message.
		<-ack
		if ctx.Err() != nil {
			return
		}
	}
}

func (r *Runtime) shutdownWorkers() {
	r.closeWorkers()
	r.queue.reset()
	finished := make(chan struct{})
	go func() { r.workersWG.Wait(); close(finished) }()
	for {
		select {
		case n := <-r.incoming:
			if n.conn != nil {
				_ = n.conn.Close()
			}
			if n.ack != nil {
				n.ack <- struct{}{}
			}
		case <-finished:
			return
		}
	}
}

func (r *Runtime) reportError(ctx context.Context, id session.Connection, err error) {
	kind := broken
	if errors.Is(err, ErrAuthentication) || errors.Is(err, transport.ErrAuthentication) {
		kind = unauthorized
	}
	r.post(ctx, notice{id: id, kind: kind})
}

func (r *Runtime) controller(ctx context.Context, id session.Connection, controls <-chan session.Effect) {
	defer r.workersWG.Done()
	for {
		select {
		case effect := <-controls:
			requestCtx, cancel := context.WithDeadline(ctx, effect.At)
			err := r.controlRequest(requestCtx, effect)
			cancel()
			if err != nil && !r.reportControlError(ctx, id, effect.Action, err) {
				return
			}
		case <-ctx.Done():
			return
		}
	}
}

func (r *Runtime) reportControlError(ctx context.Context, id session.Connection, action session.Action, err error) bool {
	kind := bindFailed
	if action == session.RequestMigration {
		kind = migrationRequestFailed
	}
	if errors.Is(err, ErrAuthentication) || errors.Is(err, transport.ErrAuthentication) {
		kind = unauthorized
	}
	return r.post(ctx, notice{id: id, kind: kind})
}

func (r *Runtime) nativeToken(ctx context.Context) (string, error) {
	token, err := r.config.Tokens.AccessToken(ctx)
	if err != nil {
		return "", err
	}
	if token == "" {
		return "", ErrAuthentication
	}
	return token, nil
}

func (r *Runtime) controlRequest(ctx context.Context, e session.Effect) error {
	token, err := r.nativeToken(ctx)
	if err != nil {
		return err
	}
	if e.Action == session.RequestMigration {
		return r.config.Control.RequestMigration(ctx, MigrationRequest{SessionID: e.SessionID, Token: token, After: e.Cursor})
	}
	return r.config.Control.Bind(ctx, BindRequest{SessionID: e.SessionID, Token: token, Migration: e.Migration, After: e.Cursor})
}

func (r *Runtime) receive(ctx context.Context, n notice) {
	defer func() {
		if n.ack != nil {
			n.ack <- struct{}{}
		}
	}()
	w := r.find(n.id)
	if w == nil {
		if n.conn != nil {
			_ = n.conn.Close()
		}
		return
	}
	switch n.kind {
	case connected:
		w.conn = n.conn
		r.handle(ctx, session.Event{Kind: session.Dialed, Connection: n.id})
	case broken:
		r.fail(ctx, n.id)
	case unauthorized:
		r.handle(ctx, session.Event{Kind: session.AuthRejected, Connection: n.id})
	case bindFailed:
		r.handle(ctx, session.Event{Kind: session.BindFailed, Connection: n.id})
	case migrationRequestFailed:
		r.handle(ctx, session.Event{Kind: session.MigrationRequestFailed, Connection: n.id})
	case frame:
		r.process(ctx, n.id, n.message)
		r.handle(ctx, session.Event{Kind: session.Activity, Connection: n.id})
	}
}
func (r *Runtime) process(ctx context.Context, id session.Connection, m *protocol.Message) {
	if protocol.Validate(m) != nil {
		r.fail(ctx, id)
		return
	}
	e := session.Event{Connection: id}
	switch m.Kind {
	case protocol.SessionStarted:
		e.Kind = session.Welcome
		e.SessionID = m.SessionId
		e.Within = time.Duration(m.BindWithinMs) * time.Millisecond
	case protocol.SessionBound:
		e.Kind = session.Bound
		e.SessionID = m.SessionId
	case protocol.Reconnect:
		r.migrate(ctx, id, m)
		return
	case protocol.HandoffReady:
		e.Kind = session.Ready
		e.Through = m.Through
		e.Migration = m.Migration
	case protocol.Keepalive:
		return
	case protocol.AuthError:
		e.Kind = session.AuthRejected
	case protocol.EventMessage:
		r.deliver(ctx, id, m)
		return
	default:
		r.fail(ctx, id)
		return
	}
	r.handle(ctx, e)
}
func (r *Runtime) setTarget(target string) bool {
	if target == "" {
		r.target = r.config.Endpoint
		return true
	}
	base, _ := url.Parse(r.config.Endpoint)
	next, err := url.Parse(target)
	if err != nil || next.Scheme != "https" || next.Host != base.Host || next.User != nil || next.Fragment != "" || next.RawQuery != "" || next.Path != "" || next.ForceQuery {
		return false
	}
	r.target = target
	return true
}
func (r *Runtime) deliver(ctx context.Context, id session.Connection, message *protocol.Message) {
	event := message.Event
	switch r.machine.Route(id, event.Sequence) {
	case session.Deliver:
		r.apply(ctx, id, event)
	case session.Buffer:
		if r.queue.push(id, event) {
			message.Event = nil
		} else {
			r.fail(ctx, id)
		}
	case session.Gap, session.Reject:
		r.fail(ctx, id)
	}
}

func (r *Runtime) apply(ctx context.Context, id session.Connection, event *protocol.Event) {
	sequence := event.Sequence
	if r.config.Consumer.Apply(event) != nil {
		r.fail(ctx, id)
		return
	}
	r.handle(ctx, session.Event{Kind: session.Applied, Connection: id, Sequence: sequence})
}

func (r *Runtime) flush(ctx context.Context) {
	for r.queue.count > 0 && r.queue.id == r.machine.Snapshot().Primary {
		id := r.queue.id
		event := r.queue.pop()
		r.flushEvent(ctx, id, event)
	}
}

func (r *Runtime) flushEvent(ctx context.Context, id session.Connection, event *protocol.Event) {
	defer event.ReturnToVTPool()
	switch r.machine.Route(id, event.Sequence) {
	case session.Deliver:
		r.apply(ctx, id, event)
	case session.Gap, session.Reject:
		r.fail(ctx, id)
	}
}

func (r *Runtime) migrate(ctx context.Context, id session.Connection, m *protocol.Message) {
	if id != r.machine.Snapshot().Primary {
		r.fail(ctx, id)
		return
	}
	if !r.machine.CanMigrate(id) {
		return
	}
	if !r.setTarget(m.Target) {
		r.handle(ctx, session.Event{Kind: session.MigrationRejected, Connection: id})
		return
	}
	r.handle(ctx, session.Event{Kind: session.Migrate, Connection: id, Migration: m.Migration, Within: time.Duration(m.WithinMs) * time.Millisecond})
}

func validAuthority(endpoint string) bool {
	u, err := url.Parse(endpoint)
	return err == nil && u.Scheme == "https" && u.Hostname() != "" && u.User == nil && u.Fragment == "" && u.RawQuery == "" && u.Path == "" && !u.ForceQuery
}
