// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package gateway

import (
	"cmp"
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	ddiscord "ItsBagelBot/internal/domain/discord"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

const (
	logSocketEnded    = "WARN discord gateway socket ended; reconnecting"
	logFatal          = "ERROR discord gateway closed fatally; not reconnecting"
	logFlapping       = "ERROR gateway flapping"
	logCeiling        = "ERROR gateway connect budget exhausted; parked until the window frees"
	logDegraded       = "WARN discord connect budget is not persisted; counting this process only"
	logResuming       = "INFO discord gateway resuming session"
	logResumed        = "INFO discord gateway session resumed"
	logInvalidated    = "INFO discord gateway invalidated session"
	logDispatchFailed = "WARN discord dispatch failed"
)

const (
	resumeDialURL = "wss://resume/?v=10&encoding=json"
	safetyWindow  = 10 * time.Second
)

type gatewayFrames struct {
	hello, fastBeat, ack, ready, resumed, join, identity, reconnect, refused, resumable []byte
}

func newFrames(t *testing.T) gatewayFrames {
	return gatewayFrames{
		hello:    helloFrame(t),
		fastBeat: frame(t, packet{Op: opHello, D: mustRaw(t, helloData{HeartbeatInterval: 10})}),
		ack:      frame(t, packet{Op: opHeartbeatAck}),
		ready: frame(t, packet{Op: opDispatch, T: eventReady, S: intPtr(4),
			D: mustRaw(t, readyData{SessionID: "sess-1", ResumeGatewayURL: "wss://resume"})}),
		resumed: dispatchPacket(t, eventResumed, struct{}{}),
		join:    dispatchPacket(t, "GUILD_MEMBER_ADD", map[string]string{"guild_id": "g1"}),
		identity: dispatchPacket(t, eventReady, map[string]any{
			"session_id":  "sess-1",
			"guilds":      []map[string]string{{"id": "g1"}, {"id": "g2"}},
			"application": map[string]string{"id": "app-1"},
			"user":        map[string]string{"id": "bot-1"},
		}),
		reconnect: frame(t, packet{Op: opReconnect}),
		refused:   frame(t, packet{Op: opInvalidSession, D: mustRaw(t, false)}),
		resumable: frame(t, packet{Op: opInvalidSession, D: mustRaw(t, true)}),
	}
}

type runCase struct {
	name       string
	scripts    []script
	url        string
	budget     *connectBudget
	connects   ConnectLog
	handlerErr error
	window     time.Duration
	stopAfter  int
	want       runSeen
}

type runSeen struct {
	URLs       []string
	Opened     []int
	Ready      []Identity
	Dispatched []string
	Events     int
	Ups        []Up
	Downs      []Down
	FinalDowns int
	Budgets    []budgetSeen
	Logs       []string
	Unbounded  int
}

func runScripted(t *testing.T, tc runCase) (runSeen, *observer.ObservedLogs) {
	t.Helper()
	core, logs := observer.New(zapcore.InfoLevel)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	timer := time.AfterFunc(cmp.Or(tc.window, safetyWindow), cancel)
	defer timer.Stop()
	d := &dialer{scripts: tc.scripts}
	h := &recHandler{err: tc.handlerErr}
	st := &recStatus{stopAfter: tc.stopAfter, stop: cancel}
	sess := Session{
		Token: "bot-token", URL: tc.url, Dial: d.dial, Handle: h, Log: zap.New(core),
		Status: st, Connects: tc.connects, budget: tc.budget,
	}

	require.ErrorIs(t, sess.Run(ctx), context.Canceled)

	return runSeen{
		URLs: d.urls, Opened: d.opened(t), Ready: h.idents, Dispatched: h.types, Events: st.events,
		Ups: st.ups, Downs: st.downs, FinalDowns: st.finals, Budgets: st.budgets, Logs: logLines(logs),
		Unbounded: d.unbounded,
	}, logs
}

func logLines(logs *observer.ObservedLogs) []string {
	var lines []string
	for _, e := range logs.All() {
		lines = append(lines, e.Level.CapitalString()+" "+e.Message)
	}
	return lines
}

var (
	closed4000    = Down{Code: 4000, Reason: errClosed.Error()}
	dropped       = Down{Reason: errClosed.Error()}
	oneBudget     = []budgetSeen{{Connects: 1}}
	twoBudgets    = []budgetSeen{{Connects: 1}, {Connects: 2}}
	reconnectable = []script{{readErr: errClosed, closeCode: 4000}}
)

func gw(dials int) []string { return slices.Repeat([]string{gatewayURL}, dials) }

func parkingCases(f gatewayFrames) []runCase {
	return []runCase{
		{
			name: "a fatal close parks without dialling again", window: 150 * time.Millisecond,
			scripts: []script{{readErr: errClosed, closeCode: ddiscord.CloseDisallowedIntents}},
			want: runSeen{URLs: gw(1), Logs: []string{logFatal},
				Downs: []Down{{Code: ddiscord.CloseDisallowedIntents, Reason: errClosed.Error(), Fatal: true}}},
		},
		{
			name: "a heartbeat write refused with a fatal close parks the session", window: 300 * time.Millisecond,
			scripts: []script{{reads: [][]byte{f.fastBeat}, writeErr: authFailure()}},
			want: runSeen{URLs: gw(1), Opened: []int{opIdentify}, Logs: []string{logFatal},
				Downs: []Down{{Code: ddiscord.CloseAuthenticationFailed, Reason: authFailure().Error(), Fatal: true}}},
		},
		{
			name: "the connect floor holds the next dial back", window: 1600 * time.Millisecond, scripts: reconnectable,
			want: runSeen{URLs: gw(1), Downs: []Down{closed4000}, Budgets: oneBudget, Logs: []string{logSocketEnded}},
		},
		{
			name: "the daily ceiling stops dialling until the window frees", window: 1500 * time.Millisecond,
			scripts: reconnectable, budget: fastBudget(1, nil),
			want: runSeen{URLs: gw(1), Downs: []Down{closed4000}, Logs: []string{logCeiling},
				Budgets: []budgetSeen{{Connects: 1, AtCeiling: true, Parked: true}}},
		},
		{
			name: "another pod's connects count toward the ceiling", stopAfter: 1, scripts: reconnectable,
			budget: fastBudget(3, &fakeConnectLog{seen: []time.Time{time.Now(), time.Now()}}),
			want: runSeen{URLs: gw(1), Downs: []Down{closed4000}, Logs: []string{logCeiling},
				Budgets: []budgetSeen{{Connects: 3, AtCeiling: true, Parked: true}}},
		},
	}
}

func budgetCases(f gatewayFrames) []runCase {
	now := time.Now()
	flappy := fastBudget(dailyConnectCeiling, nil)
	flappy.sched.flapStreak = 2
	flappy.sched.flapWait = time.Millisecond
	return []runCase{
		{
			name: "a reconnectable close dials the configured url again", stopAfter: 2, scripts: reconnectable,
			url: "ws://gateway.test/?v=10", budget: fastBudget(dailyConnectCeiling, nil),
			want: runSeen{URLs: slices.Repeat([]string{"ws://gateway.test/?v=10"}, 2), Downs: []Down{closed4000, closed4000},
				Budgets: twoBudgets, Logs: []string{logSocketEnded, logSocketEnded}},
		},
		{
			name: "dial failures count against the budget", stopAfter: 2, budget: fastBudget(dailyConnectCeiling, nil),
			scripts: []script{{dialErr: errRefused}},
			want: runSeen{URLs: gw(2), Downs: []Down{{Reason: errRefused.Error()}, {Reason: errRefused.Error()}},
				Budgets: twoBudgets, Logs: []string{logSocketEnded, logSocketEnded}},
		},
		{
			name: "short sockets in a row flag flapping until one stays up", stopAfter: 3, budget: flappy,
			scripts: []script{reconnectable[0], reconnectable[0], {reads: [][]byte{f.hello, f.ready}, readErr: errClosed, closeCode: 4000}},
			want: runSeen{URLs: gw(3), Opened: []int{opIdentify}, Ready: []Identity{{}}, Ups: []Up{{SessionID: "sess-1"}},
				Downs:   []Down{closed4000, closed4000, closed4000},
				Budgets: []budgetSeen{{Connects: 1}, {Connects: 2, Flapping: true, Parked: true}, {Connects: 3}},
				Logs:    []string{logSocketEnded, logFlapping, logSocketEnded}},
		},
		{
			name: "a restart picks up the connects spent inside the window", stopAfter: 1, scripts: reconnectable,
			connects: &fakeConnectLog{seen: []time.Time{
				now.Add(-25 * time.Hour), now.Add(-3 * time.Minute), now.Add(-2 * time.Minute), now.Add(-time.Minute),
			}},
			want: runSeen{URLs: gw(1), Downs: []Down{closed4000}, Budgets: []budgetSeen{{Connects: 4}}, Logs: []string{logSocketEnded}},
		},
		{
			name: "a store outage degrades to counting in memory with one warning", stopAfter: 1, scripts: reconnectable,
			connects: &fakeConnectLog{fail: errors.New("valkey: connection refused")},
			want:     runSeen{URLs: gw(1), Downs: []Down{closed4000}, Budgets: oneBudget, Logs: []string{logDegraded, logSocketEnded}},
		},
	}
}

func protocolCases(f gatewayFrames) []runCase {
	return []runCase{
		{
			name: "a resumed reconnect resumes at the resume url and still spends the budget", stopAfter: 2,
			budget: fastBudget(dailyConnectCeiling, nil),
			scripts: []script{
				{reads: [][]byte{f.hello, f.ready}, readErr: errClosed},
				{reads: [][]byte{f.hello, f.resumed}, readErr: errClosed},
			},
			want: runSeen{URLs: []string{gatewayURL, resumeDialURL}, Opened: []int{opIdentify, opResume}, Ready: []Identity{{}},
				Ups:   []Up{{SessionID: "sess-1"}, {SessionID: "sess-1", Resumed: true}},
				Downs: []Down{dropped, dropped}, Budgets: twoBudgets,
				Logs: []string{logSocketEnded, logResuming, logResumed, logSocketEnded}},
		},
		{
			name: "identifies, hands READY to the handler and counts dispatches and ACKs as activity", stopAfter: 1,
			budget:  fastBudget(dailyConnectCeiling, nil),
			scripts: []script{{reads: [][]byte{f.hello, f.identity, f.ack, f.join}, readErr: errClosed, closeCode: 4000}},
			want: runSeen{URLs: gw(1), Opened: []int{opIdentify}, Ready: []Identity{{ApplicationID: "app-1", BotUserID: "bot-1"}},
				Dispatched: []string{"GUILD_MEMBER_ADD"}, Events: 2, Ups: []Up{{SessionID: "sess-1", GuildCount: 2}},
				Downs: []Down{closed4000}, Budgets: oneBudget, Logs: []string{logSocketEnded}},
		},
		{
			name: "a failing handler is logged without ending the socket", stopAfter: 1,
			budget: fastBudget(dailyConnectCeiling, nil), handlerErr: errors.New("relay down"),
			scripts: []script{{reads: [][]byte{f.hello, f.join, f.join}, readErr: errClosed, closeCode: 4000}},
			want: runSeen{URLs: gw(1), Opened: []int{opIdentify}, Dispatched: []string{"GUILD_MEMBER_ADD", "GUILD_MEMBER_ADD"},
				Events: 2, Downs: []Down{closed4000}, Budgets: oneBudget,
				Logs: []string{logDispatchFailed, logDispatchFailed, logSocketEnded}},
		},
		{
			name: "a gateway reconnect request ends the socket", stopAfter: 1, budget: fastBudget(dailyConnectCeiling, nil),
			scripts: []script{{reads: [][]byte{f.hello, f.reconnect}}},
			want: runSeen{URLs: gw(1), Opened: []int{opIdentify}, Budgets: oneBudget, Logs: []string{logSocketEnded},
				Downs: []Down{{Reason: "discord ingress: gateway requested reconnect (op 7)"}}},
		},
	}
}

func sessionCases(f gatewayFrames) []runCase {
	return []runCase{
		{
			name: "a refused invalid session identifies on the next socket", stopAfter: 2,
			budget:  fastBudget(dailyConnectCeiling, nil),
			scripts: []script{{reads: [][]byte{f.hello, f.ready, f.refused}}, {reads: [][]byte{f.hello}, readErr: errClosed}},
			want: runSeen{URLs: gw(2), Opened: []int{opIdentify, opIdentify}, Ready: []Identity{{}}, Ups: []Up{{SessionID: "sess-1"}},
				Downs:   []Down{{Reason: "discord ingress: gateway invalidated session (resumable=false)"}, dropped},
				Budgets: twoBudgets, Logs: []string{logInvalidated, logSocketEnded, logSocketEnded}},
		},
		{
			name: "a resumable invalid session resumes on the next socket", stopAfter: 2,
			budget:  fastBudget(dailyConnectCeiling, nil),
			scripts: []script{{reads: [][]byte{f.hello, f.ready, f.resumable}}, {reads: [][]byte{f.hello}, readErr: errClosed}},
			want: runSeen{URLs: []string{gatewayURL, resumeDialURL}, Opened: []int{opIdentify, opResume}, Ready: []Identity{{}},
				Ups:     []Up{{SessionID: "sess-1"}},
				Downs:   []Down{{Reason: "discord ingress: gateway invalidated session (resumable=true)"}, dropped},
				Budgets: twoBudgets, Logs: []string{logInvalidated, logSocketEnded, logResuming, logSocketEnded}},
		},
		{
			name: "a heartbeat the gateway stops acknowledging ends the socket", stopAfter: 1,
			budget: fastBudget(dailyConnectCeiling, nil), scripts: []script{{reads: [][]byte{f.fastBeat}}},
			want: runSeen{URLs: gw(1), Opened: []int{opIdentify}, Downs: []Down{{Reason: errZombie.Error()}},
				Budgets: oneBudget, Logs: []string{logSocketEnded}},
		},
		{
			name: "acknowledged heartbeats keep the socket up and count as activity", window: 200 * time.Millisecond,
			scripts: []script{{reads: append([][]byte{f.fastBeat}, slices.Repeat([][]byte{f.ack}, 50)...)}},
			want: runSeen{URLs: gw(1), Opened: []int{opIdentify}, Events: 50,
				Downs: []Down{{Reason: context.Canceled.Error()}}, FinalDowns: 1},
		},
		{
			name: "shutdown reports a final down on a live context and publishes no budget", window: 60 * time.Millisecond,
			scripts: []script{{reads: [][]byte{f.hello}}},
			want:    runSeen{URLs: gw(1), Opened: []int{opIdentify}, Downs: []Down{{Reason: context.Canceled.Error()}}, FinalDowns: 1},
		},
	}
}

func TestRunDrivesTheGatewayLifecycle(t *testing.T) {
	f := newFrames(t)
	cases := slices.Concat(parkingCases(f), budgetCases(f), protocolCases(f), sessionCases(f))
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, _ := runScripted(t, tc)
			assert.Equal(t, tc.want, got)
		})
	}
}

func pick(fields, want map[string]any) map[string]any {
	got := make(map[string]any, len(want))
	for key := range want {
		got[key] = fields[key]
	}
	return got
}

func TestRunLogsWhyASocketEnded(t *testing.T) {
	f := newFrames(t)
	cases := []struct {
		name    string
		run     runCase
		message string
		want    map[string]any
	}{
		{
			name: "socket end warning carries the close telemetry",
			run: runCase{
				scripts: []script{
					{reads: [][]byte{f.hello, f.ready}, readErr: errClosed},
					{reads: [][]byte{f.hello, f.resumed}, readErr: errClosed, closeCode: 4000, closeReason: "Session is no longer valid."},
				},
				budget:    fastBudget(dailyConnectCeiling, nil),
				stopAfter: 2,
			},
			message: "discord gateway socket ended; reconnecting",
			want: map[string]any{
				"close_code": int64(4000), "close_reason": "Session is no longer valid.",
				"opened": string(openResume), "resumed": true, "session_id": "sess-1",
			},
		},
		{
			name: "invalid session log names what was refused",
			run: runCase{
				scripts: []script{
					{reads: [][]byte{f.hello, f.ready}, readErr: errClosed},
					{reads: [][]byte{f.hello, f.refused}},
				},
				budget:    fastBudget(dailyConnectCeiling, nil),
				stopAfter: 2,
			},
			message: "discord gateway invalidated session",
			want:    map[string]any{"resumable": false, "opened": string(openResume), "session_id": "sess-1"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, logs := runScripted(t, tc.run)
			entries := logs.FilterMessage(tc.message).All()
			require.NotEmpty(t, entries)
			assert.Equal(t, tc.want, pick(entries[len(entries)-1].ContextMap(), tc.want))
		})
	}
}

func TestRunRejectsAnIncompleteSession(t *testing.T) {
	dial := (&dialer{scripts: []script{{}}}).dial
	cases := []struct {
		name string
		sess Session
		want string
	}{
		{"without a token", Session{Dial: dial}, "discord ingress: empty bot token"},
		{"without a dialer", Session{Token: "bot-token"}, "discord ingress: nil dial"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.EqualError(t, tc.sess.Run(context.Background()), tc.want)
		})
	}
}
