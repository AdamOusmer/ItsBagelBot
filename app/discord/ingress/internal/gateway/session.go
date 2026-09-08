// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package gateway

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	ddiscord "ItsBagelBot/internal/domain/discord"
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
	// Close ends a socket this client means to reconnect on, leaving the
	// Discord session resumable. See gateway.reconnectingClose for why that
	// is not the same thing as a normal closure.
	Close() error
	// Shutdown ends a socket nothing will come back to, telling Discord to
	// discard the session. Only the process stopping earns it.
	Shutdown() error
	// CloseCode reports the WebSocket close code carried by err, or 0 when
	// err is not a close frame at all (a plain network drop, a decode
	// failure, a cancelled context). It hangs off the connection because
	// only the connection knows how to read a code off its own transport,
	// and the code is the entire difference between "dial again" and "a
	// human has to fix the token" -- see discord.FatalCloseCode.
	CloseCode(err error) int
	// CloseReason reports the close frame's own text, empty when err is not
	// a close frame. It hangs off the connection for the same reason
	// CloseCode does, and it is not redundant with the code: Discord sends
	// several unrelated faults as 4000 and separates them only here.
	CloseReason(err error) string
}

// openMode names how one connection opened: op 6 continues a session, op 2
// starts a fresh one. Empty means the socket died before Hello, which is
// itself a distinct fact.
//
// A named type rather than a bare string so the value cannot be spelled three
// ways in logs, and so the argument lists carrying it stay out of CodeScene's
// string-heavy count.
type openMode string

const (
	openIdentify openMode = "identify"
	openResume   openMode = "resume"
)

// Up describes a gateway connection that just came up.
type Up struct {
	SessionID string
	// Resumed separates RESUMED from READY: a resume keeps the session, its
	// presence and Discord's buffered backlog, a ready starts fresh.
	Resumed    bool
	GuildCount int
}

// Down describes a gateway connection that just died.
type Down struct {
	Code   int
	Reason string
	Fatal  bool
}

// Status observes the connection lifecycle so something outside this package
// can publish it (app/discord/ingress/internal/botstatus writes the Valkey
// key the dashboard reads). Nil disables reporting entirely: the loop
// behaves identically with or without it, which is what keeps every existing
// test wiring valid.
type Status interface {
	Up(ctx context.Context, up Up)
	Down(ctx context.Context, down Down)
	// Event notes one dispatch arriving. An open socket carrying no traffic
	// and a wedged one look identical from the outside; this is the only
	// evidence that separates them.
	Event(ctx context.Context)
	// Budget publishes the connect budget after every finished socket. A
	// reader that sees Flapping or AtCeiling knows the bot is offline
	// because this process is deliberately holding back, not because it
	// gave up or died.
	Budget(ctx context.Context, b Budget)
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

	// Status, if set, is told every time the socket comes up or goes down
	// and every time a dispatch lands. Nil is a no-op.
	Status Status

	// Connects, if set, persists the connect budget's rolling window across
	// restarts. Nil counts in this process only, which is what the budget
	// degrades to anyway when the store is unreachable -- see budget.go for
	// why a per-process count bounds nothing against a crash-loop.
	Connects ConnectLog

	// budget throttles connect attempts (see budget.go). Unexported and
	// nil in production wiring: Run builds the real one. Tests install a
	// schedule measured in milliseconds so they can assert the rules
	// without sleeping through a 5 minute flap wait.
	budget *connectBudget
}

// defaultPresenceInterval only applies if a caller wires a PresenceSource
// but forgets PresenceInterval; production wiring always sets it explicitly.
const defaultPresenceInterval = 5 * time.Minute

// Run identifies and pumps events until ctx is done, or until Discord
// closes with a code no reconnect can fix (see parkOnFatal).
func (s Session) Run(ctx context.Context) error {
	if err := s.validate(); err != nil {
		return err
	}
	url := s.gatewayURL()
	st := &resumeState{}
	rc := newReconnect()
	bud := s.connectBudget()
	for {
		bud.note()
		seq := connectSeq.Add(1)
		end := s.connect(ctx, url, st)
		end.seq = seq
		if ctx.Err() != nil {
			// Report BEFORE returning. The status key has no TTL (see
			// discord.BotStatusKey), so a pod that shut down without this
			// left connected:true behind forever and the dashboard showed a
			// green pill for a bot that no longer existed. The write needs
			// its own context: ctx is already cancelled here, and a Valkey
			// call under a cancelled context fails instantly.
			s.reportFinalDown(ctx, end.code, end.err)
			return ctx.Err()
		}
		s.reportDown(ctx, end.code, end.err)
		if ddiscord.FatalCloseCode(end.code) {
			return s.parkOnFatal(ctx, end.code, end.err)
		}
		wait := s.afterSocket(ctx, budgetInputs{bud: bud, rc: rc}, end)
		if err := waitBeforeReconnect(ctx, wait); err != nil {
			return err
		}
	}
}

// connectSeq numbers this process's connect attempts, starting at 1 and never
// resetting. It is package level rather than a Session field because Session
// is passed by value everywhere and a counter that a copy can restart answers
// nothing.
//
// The question it exists to answer: on 2026-09-07 production logged 21,575
// "socket ended; reconnecting" lines in 24h, one every 4.0s, flat, while the
// cluster showed a single pod with zero restarts. That contradicted the
// connect budget's own invariants (5s floor, 5-flap brake, 800/24h ceiling in
// budget.go), so either the budget was defeated or the line was not 1:1 with
// sockets. A run of connect_seq 1..N under one boot_id settles it: one
// process looping counts up without gaps, many processes each restart at 1.
var connectSeq atomic.Int64

// sessionEnd is one finished socket: how long it was up, what killed it, and
// what it was doing when it died.
//
// Everything past err was added for the 2026-09-07 measurement above. The old
// line printed a close code and an error and nothing else, which could not
// distinguish a resume Discord honoured from one it answered with op 9 -- the
// difference between a free reconnect and one IDENTIFY out of 1000/day. It is
// a struct rather than an argument list because afterSocket already takes two
// other values and CodeScene bounds the third at four fields of its own.
type sessionEnd struct {
	up   time.Duration
	code int
	// reason is the close frame's own text; see Conn.CloseReason for why the
	// code alone does not name the fault.
	reason string
	err    error
	// opened is which of op 2 and op 6 this connection actually sent, empty
	// when it died before Hello.
	opened openMode
	// resumed is whether RESUMED landed rather than READY. opened=resume
	// with resumed=false is a refused resume, the shape that silently spends
	// the identify allowance.
	resumed bool
	// sessionID is the session this socket was running, kept even after
	// Discord invalidated it (see resumeState.lastID).
	sessionID string
	// seq is this attempt's number within the process; see connectSeq.
	seq int64
}

// budgetInputs is the pair of schedulers Run carries across iterations. They
// travel together because afterSocket asks both and takes the longer wait;
// passing them as one value keeps the argument list short enough for the
// complexity gate.
type budgetInputs struct {
	bud *connectBudget
	rc  *reconnect
}

// afterSocket folds a dead socket into both schedules and reports how long
// to wait before the next connect.
//
// The budget floors the backoff rather than replacing it: backoff is what
// makes an ordinary blip invisible, and the budget is what makes a loop
// impossible. Whichever says "wait longer" wins.
func (s Session) afterSocket(ctx context.Context, in budgetInputs, end sessionEnd) time.Duration {
	state := in.bud.record(end.up)
	s.reportBudget(ctx, state)
	wait := in.rc.next(end.up)
	if d := in.bud.delay(); d > wait {
		wait = d
	}
	s.logSocketEnd(end, state, wait)
	return wait
}

// logSocketEnd says why the next connect is waiting. The two budget cases
// are ERROR, not WARN: they mean this process has decided to stop trying at
// the normal rate, which is exactly the fact nobody had when the token was
// reset (see budget.go).
func (s Session) logSocketEnd(end sessionEnd, state Budget, wait time.Duration) {
	fields := socketEndFields(end, state, wait)
	if state.Flapping {
		s.log().Error("gateway flapping", fields...)
		return
	}
	if state.AtCeiling {
		s.log().Error("gateway connect budget exhausted; parked until the window frees", fields...)
		return
	}
	s.log().Warn("discord gateway socket ended; reconnecting", fields...)
}

// socketEndFields is the one field set every socket-end line carries,
// whatever its level.
//
// Built once rather than per branch because the branches had drifted: the
// flap and ceiling lines named the close code, the meaning, the uptime and
// the window, while the ordinary WARN -- the one that fired 21,575 times in
// 24h on 2026-09-07 -- named a close code and an error and stopped there.
// Splitting this per branch again would let the same drift back in, and three
// sibling builders is also the shape CodeScene reads as duplication.
func socketEndFields(end sessionEnd, state Budget, wait time.Duration) []zap.Field {
	return []zap.Field{
		zap.Int("close_code", end.code),
		zap.String("close_reason", end.reason),
		zap.String("meaning", ddiscord.CloseCodeMessage(end.code)),
		zap.Duration("uptime", end.up),
		zap.String("opened", string(end.opened)),
		zap.Bool("resumed", end.resumed),
		zap.String("session_id", end.sessionID),
		zap.Int64("connect_seq", end.seq),
		zap.Int("connects_in_window", state.Connects),
		zap.Duration("wait", wait),
		zap.Error(end.err),
	}
}

// connectBudget is the production budget unless a test installed its own.
func (s Session) connectBudget() *connectBudget {
	if s.budget != nil {
		return s.budget
	}
	return newConnectBudget(s.Connects, s.log())
}

// connect runs one socket and reports how long it stayed up next to the
// close code it died with. The uptime is what resets the backoff schedule
// (see reconnect.next), and it is measured from READY/RESUMED, not from
// before the dial: a dial into a blackhole sits in the TCP/TLS handshake for
// as long as the timeout allows, and counting that as uptime let a socket
// that never carried a single byte reset the backoff and hammer Discord's
// identify budget at a steady 1s. A connection that never reached
// READY/RESUMED reports zero uptime, which is what it earned.
func (s Session) connect(ctx context.Context, url string, st *resumeState) sessionEnd {
	st.resetUp()
	end := s.oneSocket(ctx, dialURLFor(url, st), st)
	m := st.mark()
	end.up = st.upFor(time.Now())
	end.opened = m.opened
	end.resumed = m.resumed
	end.sessionID = m.sessionID
	return end
}

// parkOnFatal logs the one ERROR line for a fatal close and then blocks
// until the process is shut down.
//
// Blocking rather than returning is deliberate: main treats a Run error as
// log.Fatal, and crash-looping is strictly worse than sitting still here.
// Every fatal code (bad token, wrong intents, resharding) is fixed by a
// secret or an application-portal change, and a Doppler secret change
// restarts this pod on its own. Until that lands the pod stays up, /readyz
// unready, with the explanation still in its logs; liveness fails after
// discord.BotFatalGrace so a pod nobody ever rotates is restarted anyway.
func (s Session) parkOnFatal(ctx context.Context, code int, err error) error {
	s.log().Error("discord gateway closed fatally; not reconnecting",
		zap.Int("close_code", code),
		zap.String("meaning", ddiscord.CloseCodeMessage(code)),
		zap.Error(err))
	<-ctx.Done()
	return ctx.Err()
}

func (s Session) reportDown(ctx context.Context, code int, err error) {
	if s.Status == nil {
		return
	}
	s.Status.Down(ctx, Down{Code: code, Reason: errText(err), Fatal: ddiscord.FatalCloseCode(code)})
}

// finalDownTimeout bounds the one status write that happens after ctx is
// already cancelled. 2s matches botstatus's own per-write bound: long enough
// for a Valkey round trip including a reconnect, short enough that a Valkey
// that is itself down cannot hold the pod past its termination grace.
const finalDownTimeout = 2 * time.Second

// reportFinalDown writes the shutdown transition under a context detached
// from the cancelled one. WithoutCancel keeps the trace and any values on it
// while dropping the cancellation, which is the whole point: the deadline
// below is the only thing that may stop this write.
func (s Session) reportFinalDown(ctx context.Context, code int, err error) {
	if s.Status == nil {
		return
	}
	wctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), finalDownTimeout)
	defer cancel()
	s.reportDown(wctx, code, err)
}

func (s Session) reportBudget(ctx context.Context, b Budget) {
	if s.Status == nil {
		return
	}
	s.Status.Budget(ctx, b)
}

func (s Session) reportUp(ctx context.Context, up Up) {
	if s.Status == nil {
		return
	}
	s.Status.Up(ctx, up)
}

func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// validate reports the two misconfigurations Run cannot recover from by
// retrying: an empty token or a nil Dial would just fail identically on
// every reconnect attempt.
func (s Session) validate() error {
	if s.Token == "" {
		return fmt.Errorf("discord ingress: empty bot token")
	}
	if s.Dial == nil {
		return fmt.Errorf("discord ingress: nil dial")
	}
	return nil
}

// gatewayURL is the configured URL, or the package default when unset.
func (s Session) gatewayURL() string {
	if s.URL == "" {
		return gatewayURL
	}
	return s.URL
}

// dialURLFor picks the socket url for one connection attempt. A resume must
// go to the URL READY handed back, not the ordinary gateway URL; Discord does
// not guarantee the latter works for one.
func dialURLFor(url string, st *resumeState) string {
	if _, resumeURL, ok := st.resumable(); ok && resumeURL != "" {
		return resumeURL
	}
	return url
}

// waitBeforeReconnect pauses for d between reconnect attempts, returning
// ctx's own error if it is cancelled first so Run's caller sees the real
// reason it stopped rather than a timer firing.
func waitBeforeReconnect(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// dialTimeout bounds one handshake. Without it a dial inherits only Run's
// context, which lives for the life of the process: a gateway host that
// accepts the TCP connection and then answers nothing (a blackholed route,
// a half-open NAT entry) parked the whole ingress in Dial indefinitely, with
// the status key still claiming whatever the previous socket left there.
// 30s is well past Discord's own handshake latency (tens of ms) and past a
// slow TLS negotiation on a congested node, so it only ever fires on a
// connection that was never going to complete.
const dialTimeout = 30 * time.Second

// oneSocket dials, pumps, and reports how the socket died. The code and the
// reason are read off the connection before the deferred Close runs, because
// Close is what replaces a peer's close frame with our own.
func (s Session) oneSocket(ctx context.Context, url string, st *resumeState) sessionEnd {
	conn, err := s.dialConn(ctx, url)
	if err != nil {
		return sessionEnd{err: err}
	}
	defer func() { _ = endSocket(ctx, conn) }()
	perr := s.pump(ctx, conn, st)
	return sessionEnd{code: conn.CloseCode(perr), reason: conn.CloseReason(perr), err: perr}
}

// endSocket closes the connection the way the reason for closing demands. A
// cancelled context is the only evidence this process has that it is stopping
// rather than reconnecting, and that difference decides whether Discord keeps
// the session for the next socket -- see reconnectingClose.
func endSocket(ctx context.Context, conn Conn) error {
	if ctx.Err() != nil {
		return conn.Shutdown()
	}
	return conn.Close()
}

// dialConn bounds the handshake and nothing else: the deadline is released
// as soon as Dial returns, because a Conn must outlive the context that
// opened it (coder/websocket's Dial uses its context for the handshake
// only, and cancelling it afterwards does not touch the connection).
func (s Session) dialConn(ctx context.Context, url string) (Conn, error) {
	dctx, cancel := context.WithTimeout(ctx, dialTimeout)
	defer cancel()
	return s.Dial(dctx, url)
}

func (s Session) pump(ctx context.Context, conn Conn, st *resumeState) error {
	sk := newSocket(conn)
	defer close(sk.stop)
	for {
		pkt, err := readPacket(ctx, conn)
		if err != nil {
			return sk.firstError(err)
		}
		// Record the sequence before handling: it is what a resume replays
		// from and what the heartbeat reports, and both must reflect
		// everything received even if handling this packet fails.
		st.note(pkt.S)
		if err := s.handlePacket(ctx, sk, pkt, st); err != nil {
			return sk.firstError(err)
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

func (s Session) handlePacket(ctx context.Context, sk *socket, pkt packet, st *resumeState) error {
	switch pkt.Op {
	case opHello:
		return s.onHello(ctx, sk, pkt, st)
	case opHeartbeatAck:
		// An ACK is Discord answering on a socket that is otherwise silent,
		// and it is the only traffic a bot in a quiet guild sees. It counts
		// as an event so the liveness clock (discord.BotEventMaxAge)
		// measures "is this socket delivering", not "is this guild busy".
		s.noteEvent(ctx)
		return nil
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
	// The line that says whether the op 6 this socket sent was refused.
	// Discord answers op 9 to a stale resume and to a rejected identify
	// alike, and only "opened" separates them; a d:false answer to a resume
	// means the next connect spends a full IDENTIFY out of 1000/day. None of
	// that was visible in the 21,575 socket-end lines of 2026-09-07, which is
	// how a reconnect loop hid inside a budget built to bound one.
	m := st.mark()
	s.log().Info("discord gateway invalidated session",
		zap.Bool("resumable", resumable),
		zap.String("opened", string(m.opened)),
		zap.String("session_id", m.sessionID))
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

func (s Session) onHello(ctx context.Context, sk *socket, pkt packet, st *resumeState) error {
	var hello helloData
	if err := codec.Unmarshal(pkt.D, &hello); err != nil {
		return err
	}
	identified, err := s.openSession(ctx, sk.conn, st)
	if err != nil {
		return err
	}
	go s.heartbeat(ctx, sk, hello.HeartbeatInterval, st)
	// Presence is forced only after an Identify. A resumed session keeps the
	// activity it already had, so re-sending it there would spend one of
	// Discord's 5-per-20s presence updates to set what is already set.
	go s.presenceLoop(ctx, sk, identified)
	return nil
}

// openSession sends Resume when a session survives, Identify otherwise, and
// reports which happened. The two are not interchangeable: Identify starts a
// fresh session and discards whatever Discord buffered during the gap, while
// Resume replays it from the last sequence.
func (s Session) openSession(ctx context.Context, conn Conn, st *resumeState) (identified bool, err error) {
	sessionID, _, ok := st.resumable()
	if !ok {
		st.markOpened(openIdentify)
		return true, writeJSON(ctx, conn, identifyBody(s.Token))
	}
	st.markOpened(openResume)
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
func (s Session) presenceLoop(ctx context.Context, sk *socket, force bool) {
	if s.Presence == nil {
		return
	}
	interval := s.PresenceInterval
	if interval <= 0 {
		interval = defaultPresenceInterval
	}
	s.sendPresence(ctx, sk, force)
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-sk.stop:
			return
		case <-t.C:
			s.sendPresence(ctx, sk, false)
		}
	}
}

// sendPresence asks Presence for the current status and writes it if there
// is anything to send. force clears Presence's own dedup first (see
// PresenceSource.Forget) so a reconnect resends even an unchanged count; a
// plain ticker tick leaves the dedup alone so an unchanged count sends
// nothing, staying well inside Discord's 5-updates-per-20s budget.
func (s Session) sendPresence(ctx context.Context, sk *socket, force bool) {
	if force {
		s.Presence.Forget()
	}
	name, ok := s.Presence.Refresh(ctx)
	if !ok {
		return
	}
	// socket.write, not writeJSON: it funnels the failure back to the pump
	// (a write that fails here failed on the same socket the heartbeat
	// writes to, and it may be carrying the close frame that explains why)
	// and it is what makes this goroutine visible to firstError's grace
	// while the write is still in flight. Warn only -- presence is cosmetic.
	if err := sk.write(ctx, presenceUpdateBody(name)); err != nil {
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
		sessionID, _, _ := st.resumable()
		st.markUp(time.Now(), true)
		s.reportUp(ctx, Up{SessionID: sessionID, Resumed: true})
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
	st.markUp(time.Now(), false)
	s.reportUp(ctx, Up{SessionID: ready.SessionID, GuildCount: len(ready.Guilds)})
	if s.Handle == nil {
		return nil
	}
	return s.Handle.Ready(ctx, Identity{ApplicationID: ready.Application.ID, BotUserID: ready.User.ID})
}

// noteEvent tells Status something arrived on this socket. Both a dispatch
// and a heartbeat ACK go through it, because the question it answers is
// "did the socket deliver anything", not "did the guild do anything".
func (s Session) noteEvent(ctx context.Context) {
	if s.Status == nil {
		return
	}
	s.Status.Event(ctx)
}

func (s Session) dispatchEvent(ctx context.Context, pkt packet) error {
	s.noteEvent(ctx)
	if s.Handle == nil {
		return nil
	}
	return s.Handle.Dispatch(ctx, Event{Type: pkt.T, Raw: pkt.D})
}

func (s Session) heartbeat(ctx context.Context, sk *socket, intervalMS int, st *resumeState) {
	if intervalMS <= 0 {
		return
	}
	t := time.NewTicker(time.Duration(intervalMS) * time.Millisecond)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-sk.stop:
			return
		case <-t.C:
			// The last received sequence, not nil. Discord compares this
			// against what it sent to notice a client has fallen behind;
			// a permanent null claims nothing was ever received.
			// socket.write funnels the error to the pump. Returning silently
			// here was the bug: this goroutine is where a fatal close frame
			// most often lands, and dropping the error left the pump to
			// report a codeless "connection closed" that reconnected
			// forever. See socket.firstError.
			if err := sk.write(ctx, heartbeatBody(st.sequence())); err != nil {
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
