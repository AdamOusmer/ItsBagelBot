// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package gateway

import (
	"context"
	"fmt"
	"time"

	"ItsBagelBot/pkg/codec"

	"go.uber.org/zap"
)

// Handler receives one dispatched gateway event.
type Handler interface {
	Dispatch(ctx context.Context, ev Event) error
	Ready(ctx context.Context, ident Identity) error
}

// Event is one Discord gateway dispatch payload.
type Event struct {
	Type string
	Raw  []byte
}

// Identity is the application and bot user from READY.
type Identity struct {
	ApplicationID string
	BotUserID     string
}

// Conn is one WebSocket. Tests inject a scripted implementation.
type Conn interface {
	Read(ctx context.Context) ([]byte, error)
	Write(ctx context.Context, data []byte) error
	Close() error
}

// Dial opens a gateway WebSocket.
type Dial func(ctx context.Context, url string) (Conn, error)

// PresenceSource supplies the bot's Discord activity status. Session owns
// sending it because sending it needs the live gateway socket (Update
// Presence, op 3), which only Session holds -- see internal/domain/discord's
// Event doc for why ingress otherwise never acts on anything, and this
// package doc's presenceLoop for why presence is the second deliberate
// exception (the interaction defer in relay/ack.go is the first).
type PresenceSource interface {
	// Refresh reports the activity name to send ("1,234 streams"), or
	// ok=false when nothing should go out right now: the value is unchanged
	// since the last successful send, or computing it failed. A failure is
	// swallowed here, not returned as an error, because presence is
	// cosmetic -- an RPC hiccup must never stall the heartbeat/dispatch loop
	// it shares a goroutine budget with, only skip one status refresh.
	Refresh(ctx context.Context) (name string, ok bool)
	// Forget clears any dedup state so the next Refresh reports ok=true even
	// when the value has not changed.
	Forget()
}

// Session is the long-lived Discord gateway loop: Hello, Identify,
// heartbeat, dispatch. A dropped socket reconnects until ctx is cancelled.
type Session struct {
	Token  string
	Dial   Dial
	Handle Handler
	Log    *zap.Logger
	URL    string

	// Presence, if set, is refreshed once immediately after every successful
	// Identify and again on every PresenceInterval tick thereafter. Nil
	// disables presence entirely (no field wired, no behavior change).
	Presence PresenceSource
	// PresenceInterval paces the ticker; see the constant that feeds it
	// (app/discord/ingress/internal/presence.RefreshInterval) for why that
	// value. Zero/negative falls back to defaultPresenceInterval.
	PresenceInterval time.Duration
}

// defaultPresenceInterval only applies if a caller wires a PresenceSource
// but forgets PresenceInterval; production wiring always sets it explicitly.
const defaultPresenceInterval = 5 * time.Minute

// Run identifies and pumps events until ctx is done.
func (s Session) Run(ctx context.Context) error {
	if s.Token == "" {
		return fmt.Errorf("discord ingress: empty bot token")
	}
	if s.Dial == nil {
		return fmt.Errorf("discord ingress: nil dial")
	}
	url := s.URL
	if url == "" {
		url = gatewayURL
	}
	for {
		err := s.oneSocket(ctx, url)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		s.log().Warn("discord gateway socket ended; reconnecting", zap.Error(err))
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
}

func (s Session) oneSocket(ctx context.Context, url string) error {
	conn, err := s.Dial(ctx, url)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()
	return s.pump(ctx, conn)
}

func (s Session) pump(ctx context.Context, conn Conn) error {
	beats := make(chan struct{}, 1)
	defer close(beats)
	for {
		pkt, err := readPacket(ctx, conn)
		if err != nil {
			return err
		}
		if err := s.handlePacket(ctx, conn, pkt, beats); err != nil {
			return err
		}
	}
}

func readPacket(ctx context.Context, conn Conn) (packet, error) {
	raw, err := conn.Read(ctx)
	if err != nil {
		return packet{}, err
	}
	var pkt packet
	if err := codec.Unmarshal(raw, &pkt); err != nil {
		return packet{}, fmt.Errorf("discord ingress: decode gateway packet: %w", err)
	}
	return pkt, nil
}

func (s Session) handlePacket(ctx context.Context, conn Conn, pkt packet, beats chan struct{}) error {
	switch pkt.Op {
	case opHello:
		return s.onHello(ctx, conn, pkt, beats)
	case opReconnect, opInvalidSession:
		return fmt.Errorf("discord ingress: gateway requested reconnect (op %d)", pkt.Op)
	case opDispatch:
		s.warnDispatch(ctx, pkt)
		return nil
	default:
		return nil
	}
}

func (s Session) warnDispatch(ctx context.Context, pkt packet) {
	if err := s.onDispatch(ctx, pkt); err != nil {
		s.log().Warn("discord dispatch failed", zap.String("t", pkt.T), zap.Error(err))
	}
}

func (s Session) onHello(ctx context.Context, conn Conn, pkt packet, beats chan struct{}) error {
	var hello helloData
	if err := codec.Unmarshal(pkt.D, &hello); err != nil {
		return err
	}
	if err := writeJSON(ctx, conn, identifyBody(s.Token)); err != nil {
		return err
	}
	go s.heartbeat(ctx, conn, hello.HeartbeatInterval, beats)
	go s.presenceLoop(ctx, conn, beats)
	return nil
}

// presenceLoop resends the bot's activity status on this socket. It hooks
// here, alongside heartbeat, because Hello->Identify is the one point in the
// gateway lifecycle that fires exactly once per connection AND every
// reconnect (this Session never resumes -- opReconnect/opInvalidSession both
// fall through to a brand new socket and a brand new Identify, see
// handlePacket): that is exactly "every successful connect", the moment
// constraint the presence feature needs, with no extra signal to invent.
//
// The immediate, forced send below is what makes presence survive a
// reconnect: a fresh IDENTIFY otherwise starts the session with no activity,
// and silently sitting blank until the next ticker fire (up to
// PresenceInterval later) is the exact failure mode this loop exists to
// close. beats is heartbeat's own stop channel, reused rather than plumbing a
// second one: closing it (pump's defer) ends both goroutines together when
// this socket dies, which is correct -- there is nothing left to refresh
// presence on until the next Identify starts a new presenceLoop.
func (s Session) presenceLoop(ctx context.Context, conn Conn, stop <-chan struct{}) {
	if s.Presence == nil {
		return
	}
	interval := s.PresenceInterval
	if interval <= 0 {
		interval = defaultPresenceInterval
	}
	s.sendPresence(ctx, conn, true)
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-stop:
			return
		case <-t.C:
			s.sendPresence(ctx, conn, false)
		}
	}
}

// sendPresence asks Presence for the current status and writes it if there
// is anything to send. force clears Presence's own dedup first (see
// PresenceSource.Forget) so a reconnect resends even an unchanged count; a
// plain ticker tick leaves the dedup alone so an unchanged count sends
// nothing, staying well inside Discord's 5-updates-per-20s budget.
func (s Session) sendPresence(ctx context.Context, conn Conn, force bool) {
	if force {
		s.Presence.Forget()
	}
	name, ok := s.Presence.Refresh(ctx)
	if !ok {
		return
	}
	if err := writeJSON(ctx, conn, presenceUpdateBody(name)); err != nil {
		s.log().Warn("discord presence update failed", zap.Error(err))
	}
}

func (s Session) onDispatch(ctx context.Context, pkt packet) error {
	if pkt.T == eventReady {
		return s.readyFrom(ctx, pkt)
	}
	return s.dispatchEvent(ctx, pkt)
}

func (s Session) readyFrom(ctx context.Context, pkt packet) error {
	var ready readyData
	if err := codec.Unmarshal(pkt.D, &ready); err != nil {
		return err
	}
	if s.Handle == nil {
		return nil
	}
	return s.Handle.Ready(ctx, Identity{ApplicationID: ready.Application.ID, BotUserID: ready.User.ID})
}

func (s Session) dispatchEvent(ctx context.Context, pkt packet) error {
	if s.Handle == nil {
		return nil
	}
	return s.Handle.Dispatch(ctx, Event{Type: pkt.T, Raw: pkt.D})
}

func (s Session) heartbeat(ctx context.Context, conn Conn, intervalMS int, stop <-chan struct{}) {
	if intervalMS <= 0 {
		return
	}
	t := time.NewTicker(time.Duration(intervalMS) * time.Millisecond)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-stop:
			return
		case <-t.C:
			if err := writeJSON(ctx, conn, heartbeatBody(nil)); err != nil {
				return
			}
		}
	}
}

func (s Session) log() *zap.Logger {
	if s.Log != nil {
		return s.Log
	}
	return zap.NewNop()
}

func writeJSON(ctx context.Context, conn Conn, v any) error {
	raw, err := codec.Marshal(v)
	if err != nil {
		return err
	}
	return conn.Write(ctx, raw)
}
