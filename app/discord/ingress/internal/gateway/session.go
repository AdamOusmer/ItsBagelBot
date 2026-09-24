// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package gateway

import (
	"context"
	"errors"
	"fmt"
	neturl "net/url"
	"sync/atomic"
	"time"

	ddiscord "ItsBagelBot/internal/domain/discord"
	"ItsBagelBot/pkg/codec"

	"go.uber.org/zap"
)

type Handler interface {
	Dispatch(ctx context.Context, ev Event) error
	Ready(ctx context.Context, ident Identity) error
}

type Event struct {
	Type string
	Raw  []byte
}

type Identity struct {
	ApplicationID string
	BotUserID     string
}

type Conn interface {
	Read(ctx context.Context) ([]byte, error)
	Write(ctx context.Context, data []byte) error
	Close() error
	Shutdown() error
	CloseCode(err error) int
	CloseReason(err error) string
}

type openMode string

const (
	openIdentify openMode = "identify"
	openResume   openMode = "resume"
)

type Up struct {
	SessionID  string
	Resumed    bool
	GuildCount int
}

type Down struct {
	Code   int
	Reason string
	Fatal  bool
}

type Status interface {
	Up(ctx context.Context, up Up)
	Down(ctx context.Context, down Down)
	Event(ctx context.Context)
	Budget(ctx context.Context, b Budget)
}

type Dial func(ctx context.Context, url string) (Conn, error)

type PresenceSource interface {
	Refresh(ctx context.Context) (name string, ok bool)
	Forget()
}

type Session struct {
	Token  string
	Dial   Dial
	Handle Handler
	Log    *zap.Logger
	URL    string

	Presence         PresenceSource
	PresenceInterval time.Duration

	Status Status

	Connects ConnectLog

	budget *connectBudget
}

const defaultPresenceInterval = 5 * time.Minute

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

var connectSeq atomic.Int64

type sessionEnd struct {
	up        time.Duration
	code      int
	reason    string
	err       error
	opened    openMode
	resumed   bool
	sessionID string
	seq       int64
}

type budgetInputs struct {
	bud *connectBudget
	rc  *reconnect
}

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

func (s Session) connectBudget() *connectBudget {
	if s.budget != nil {
		return s.budget
	}
	return newConnectBudget(s.Connects, s.log())
}

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

const finalDownTimeout = 2 * time.Second

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

func (s Session) validate() error {
	if s.Token == "" {
		return fmt.Errorf("discord ingress: empty bot token")
	}
	if s.Dial == nil {
		return fmt.Errorf("discord ingress: nil dial")
	}
	return nil
}

func (s Session) gatewayURL() string {
	if s.URL == "" {
		return gatewayURL
	}
	return s.URL
}

func dialURLFor(url string, st *resumeState) string {
	if _, resumeURL, ok := st.resumable(); ok && resumeURL != "" {
		return withGatewayQuery(resumeURL, url)
	}
	return url
}

func withGatewayQuery(resumeURL, gatewayURL string) string {
	base, err := neturl.Parse(gatewayURL)
	if err != nil || base.RawQuery == "" {
		return resumeURL
	}
	u, err := neturl.Parse(resumeURL)
	if err != nil || u.RawQuery != "" {
		return resumeURL
	}
	if u.Path == "" {
		u.Path = "/"
	}
	u.RawQuery = base.RawQuery
	return u.String()
}

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

const dialTimeout = 30 * time.Second

func (s Session) oneSocket(ctx context.Context, url string, st *resumeState) sessionEnd {
	conn, err := s.dialConn(ctx, url)
	if err != nil {
		return sessionEnd{err: err}
	}
	defer func() { _ = endSocket(ctx, conn) }()
	perr := s.pump(ctx, conn, st)
	return sessionEnd{code: conn.CloseCode(perr), reason: conn.CloseReason(perr), err: perr}
}

func endSocket(ctx context.Context, conn Conn) error {
	if ctx.Err() != nil {
		return conn.Shutdown()
	}
	return conn.Close()
}

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
		sk.noteAck()
		s.noteEvent(ctx)
		return nil
	case opReconnect:
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

func (s Session) onInvalidSession(pkt packet, st *resumeState) error {
	var resumable bool
	if err := codec.Unmarshal(pkt.D, &resumable); err != nil {
		resumable = false
	}
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
	go s.presenceLoop(ctx, sk, identified)
	return nil
}

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

func (s Session) sendPresence(ctx context.Context, sk *socket, force bool) {
	if force {
		s.Presence.Forget()
	}
	name, ok := s.Presence.Refresh(ctx)
	if !ok {
		return
	}
	if err := sk.write(ctx, presenceUpdateBody(name)); err != nil {
		s.log().Warn("discord presence update failed", zap.Error(err))
	}
}

func (s Session) onDispatch(ctx context.Context, pkt packet, st *resumeState) error {
	switch pkt.T {
	case eventReady:
		return s.readyFrom(ctx, pkt, st)
	case eventResumed:
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
	interval := time.Duration(intervalMS) * time.Millisecond
	if !waitJitter(ctx, sk, interval) {
		return
	}
	s.beatLoop(ctx, sk, interval, st)
}

func waitJitter(ctx context.Context, sk *socket, interval time.Duration) bool {
	t := time.NewTimer(fullJitter(interval))
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-sk.stop:
		return false
	case <-t.C:
		return true
	}
}

func (s Session) beatLoop(ctx context.Context, sk *socket, interval time.Duration, st *resumeState) {
	// Beat right after the jitter: waiting another interval misses Discord's heartbeat deadline.
	beats := int64(0)
	if !s.beat(ctx, sk, st, beats) {
		return
	}
	beats++
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-sk.stop:
			return
		case <-t.C:
			if !s.beat(ctx, sk, st, beats) {
				return
			}
			beats++
		}
	}
}

var errZombie = errors.New("discord ingress: gateway stopped acknowledging heartbeats")

func (s Session) beat(ctx context.Context, sk *socket, st *resumeState, sent int64) bool {
	if sk.stale(sent) {
		sk.writeFailed(errZombie)
		return false
	}
	return sk.write(ctx, heartbeatBody(st.sequence())) == nil
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
