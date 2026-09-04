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
	st := &resumeState{}
	for {
		// A resume must go to the URL READY handed back, not the ordinary
		// gateway URL; Discord does not guarantee the latter works for one.
		dialURL := url
		if _, resumeURL, ok := st.resumable(); ok && resumeURL != "" {
			dialURL = resumeURL
		}
		err := s.oneSocket(ctx, dialURL, st)
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

func (s Session) oneSocket(ctx context.Context, url string, st *resumeState) error {
	conn, err := s.Dial(ctx, url)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()
	return s.pump(ctx, conn, st)
}

func (s Session) pump(ctx context.Context, conn Conn, st *resumeState) error {
	beats := make(chan struct{}, 1)
	defer close(beats)
	for {
		pkt, err := readPacket(ctx, conn)
		if err != nil {
			return err
		}
		// Record the sequence before handling: it is what a resume replays
		// from and what the heartbeat reports, and both must reflect
		// everything received even if handling this packet fails.
		st.note(pkt.S)
		if err := s.handlePacket(ctx, conn, pkt, beats, st); err != nil {
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

func (s Session) handlePacket(ctx context.Context, conn Conn, pkt packet, beats chan struct{}, st *resumeState) error {
	switch pkt.Op {
	case opHello:
		return s.onHello(ctx, conn, pkt, beats, st)
	case opReconnect:
		// Reconnect is Discord asking politely; the session stays valid, so
		// the state is kept and the next socket resumes into it.
		return fmt.Errorf("discord ingress: gateway requested reconnect (op %d)", pkt.Op)
	case opInvalidSession:
		return s.onInvalidSession(pkt, st)
	case opDispatch:
		s.warnDispatch(ctx, pkt, st)
		return nil
	default:
		return nil
	}
}

// onInvalidSession ends the socket, first deciding whether the session
// survives. Discord sends d:true when the session is still resumable and
// d:false when it is not; a malformed or absent d is treated as NOT
// resumable, because retrying a resume Discord will refuse again just loops
// while events pile up unread.
func (s Session) onInvalidSession(pkt packet, st *resumeState) error {
	var resumable bool
	if err := codec.Unmarshal(pkt.D, &resumable); err != nil {
		resumable = false
	}
	if !resumable {
		st.invalidate()
	}
	return fmt.Errorf("discord ingress: gateway invalidated session (resumable=%t)", resumable)
}

func (s Session) warnDispatch(ctx context.Context, pkt packet, st *resumeState) {
	if err := s.onDispatch(ctx, pkt, st); err != nil {
		s.log().Warn("discord dispatch failed", zap.String("t", pkt.T), zap.Error(err))
	}
}

func (s Session) onHello(ctx context.Context, conn Conn, pkt packet, beats chan struct{}, st *resumeState) error {
	var hello helloData
	if err := codec.Unmarshal(pkt.D, &hello); err != nil {
		return err
	}
	identified, err := s.openSession(ctx, conn, st)
	if err != nil {
		return err
	}
	go s.heartbeat(ctx, conn, hello.HeartbeatInterval, beats, st)
	// Presence is forced only after an Identify. A resumed session keeps the
	// activity it already had, so re-sending it there would spend one of
	// Discord's 5-per-20s presence updates to set what is already set.
	go s.presenceLoop(ctx, conn, beats, identified)
	return nil
}

// openSession sends Resume when a session survives, Identify otherwise, and
// reports which happened. The two are not interchangeable: Identify starts a
// fresh session and discards whatever Discord buffered during the gap, while
// Resume replays it from the last sequence.
func (s Session) openSession(ctx context.Context, conn Conn, st *resumeState) (identified bool, err error) {
	sessionID, _, ok := st.resumable()
	if !ok {
		return true, writeJSON(ctx, conn, identifyBody(s.Token))
	}
	s.log().Info("discord gateway resuming session", zap.String("session_id", sessionID))
	return false, writeJSON(ctx, conn, resumeBody(s.Token, sessionID, st.sequence()))
}

// presenceLoop resends the bot's activity status on this socket. It hooks
// here, alongside heartbeat, because Hello is the one point in the gateway
// lifecycle that fires exactly once per connection, whether that connection
// goes on to Identify or to Resume.
//
// force is what makes presence survive a RECONNECT: a fresh Identify starts
// the session with no activity at all, and sitting blank until the next
// ticker fire (up to PresenceInterval later) is the failure mode this loop
// exists to close. A resumed session is the opposite case -- it keeps the
// activity it already had, so forcing there would spend one of Discord's
// 5-per-20s presence updates writing what is already written. openSession
// decides which happened and passes it through.
//
// beats is heartbeat's own stop channel, reused rather than plumbing a
// second one: closing it (pump's defer) ends both goroutines together when
// this socket dies, which is correct -- there is nothing left to refresh
// presence on until the next Hello starts a new presenceLoop.
func (s Session) presenceLoop(ctx context.Context, conn Conn, stop <-chan struct{}, force bool) {
	if s.Presence == nil {
		return
	}
	interval := s.PresenceInterval
	if interval <= 0 {
		interval = defaultPresenceInterval
	}
	s.sendPresence(ctx, conn, force)
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

func (s Session) onDispatch(ctx context.Context, pkt packet, st *resumeState) error {
	switch pkt.T {
	case eventReady:
		return s.readyFrom(ctx, pkt, st)
	case eventResumed:
		// Everything buffered during the gap has now been replayed onto this
		// socket as ordinary dispatches. Nothing to do but say so.
		s.log().Info("discord gateway session resumed")
		return nil
	}
	return s.dispatchEvent(ctx, pkt)
}

func (s Session) readyFrom(ctx context.Context, pkt packet, st *resumeState) error {
	var ready readyData
	if err := codec.Unmarshal(pkt.D, &ready); err != nil {
		return err
	}
	st.ready(ready.SessionID, ready.ResumeGatewayURL)
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

func (s Session) heartbeat(ctx context.Context, conn Conn, intervalMS int, stop <-chan struct{}, st *resumeState) {
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
			// The last received sequence, not nil. Discord compares this
			// against what it sent to notice a client has fallen behind;
			// a permanent null claims nothing was ever received.
			if err := writeJSON(ctx, conn, heartbeatBody(st.sequence())); err != nil {
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
