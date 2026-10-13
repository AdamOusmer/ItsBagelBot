// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package gateway

import (
	"context"
	"errors"
	"testing"
	"time"

	"ItsBagelBot/pkg/codec"

	"github.com/coder/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type activity struct {
	Name string `json:"name"`
	Type int    `json:"type"`
}

type presenceSeen struct {
	FirstOp int
	Frames  [][]activity
	Forgets int
}

func watching(names ...string) [][]activity {
	frames := make([][]activity, 0, len(names))
	for _, name := range names {
		frames = append(frames, []activity{{Name: name, Type: activityTypeWatching}})
	}
	return frames
}

func presenceFrames(wrote [][]byte) [][]activity {
	var frames [][]activity
	for _, raw := range wrote {
		var pkt struct {
			Op int `json:"op"`
			D  struct {
				Activities []activity `json:"activities"`
			} `json:"d"`
		}
		if codec.Unmarshal(raw, &pkt) == nil && pkt.Op == opPresenceUpdate {
			frames = append(frames, pkt.D.Activities)
		}
	}
	return frames
}

func presenceSettled(pres *fakePresence, d *dialer, refreshes, frames int) bool {
	seen, _ := pres.counts()
	return seen >= refreshes && len(presenceFrames(d.wrote(0))) >= frames
}

func runInBackground(t *testing.T, sess Session) (stop func()) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	done := make(chan error, 1)
	go func() { done <- sess.Run(ctx) }()
	return func() {
		cancel()
		require.ErrorIs(t, <-done, context.Canceled)
	}
}

func TestPresenceIsSentOnConnectAndOnChange(t *testing.T) {
	cases := []struct {
		name      string
		sends     int
		interval  time.Duration
		refreshes int
		want      presenceSeen
	}{
		{
			name: "sends once on connect after forgetting the last status", sends: 100, interval: time.Hour, refreshes: 1,
			want: presenceSeen{FirstOp: opIdentify, Frames: watching("watch-1 streams"), Forgets: 1},
		},
		{
			name: "sends each change the ticker finds", sends: 2, interval: 20 * time.Millisecond, refreshes: 2,
			want: presenceSeen{FirstOp: opIdentify, Frames: watching("watch-1 streams", "watch-2 streams"), Forgets: 1},
		},
		{
			name: "sends nothing while the count is unchanged", interval: 15 * time.Millisecond, refreshes: 2,
			want: presenceSeen{FirstOp: opIdentify, Forgets: 1},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pres := &fakePresence{sends: tc.sends}
			d := &dialer{scripts: []script{{reads: [][]byte{helloFrame(t)}}}}
			stop := runInBackground(t, Session{Token: "bot-token", Dial: d.dial, Presence: pres, PresenceInterval: tc.interval})

			require.Eventually(t, func() bool {
				return presenceSettled(pres, d, tc.refreshes, len(tc.want.Frames))
			}, 2*time.Second, 5*time.Millisecond)
			stop()

			_, forgets := pres.counts()
			wrote := d.wrote(0)
			assert.Equal(t, tc.want, presenceSeen{FirstOp: opsWritten(t, wrote)[0], Frames: presenceFrames(wrote), Forgets: forgets})
		})
	}
}

func TestSessionIdentifiesWithoutAStoredSession(t *testing.T) {
	conn := &scriptedConn{reads: [][]byte{helloFrame(t)}}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	sess := Session{Token: "t", Dial: func(context.Context, string) (Conn, error) { return conn, nil }, Handle: &recHandler{}}
	_ = sess.oneSocket(ctx, "ws://x", &resumeState{})
	if ops := opsWritten(t, conn.wroteSnapshot()); !firstOpIs(ops, opIdentify) {
		t.Fatalf("first frame ops = %v, want Identify (%d) first", ops, opIdentify)
	}
}

func TestSessionResumesAfterReady(t *testing.T) {
	ready, err := codec.Marshal(packet{
		Op: opDispatch, T: eventReady, S: intPtr(7),
		D: mustRaw(t, readyData{SessionID: "sess-1", ResumeGatewayURL: "ws://resume"}),
	})
	if err != nil {
		t.Fatalf("marshal ready: %v", err)
	}
	st := &resumeState{}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	first := &scriptedConn{reads: [][]byte{helloFrame(t), ready}, readErr: errors.New("websocket closed")}
	sess := Session{Token: "t", Dial: func(context.Context, string) (Conn, error) { return first, nil }, Handle: &recHandler{}}
	_ = sess.oneSocket(ctx, "ws://x", st)

	wantCloseCode(t, first.closeCodes(), reconnectingClose)

	sessionID, resumeURL, ok := st.resumable()
	got := resumeSnapshot{SessionID: sessionID, ResumeURL: resumeURL, OK: ok}
	if !matchesResumeState(got, resumeSnapshot{SessionID: "sess-1", ResumeURL: "ws://resume", OK: true}) {
		t.Fatalf("resume state = (%q, %q, %t), want the ids from READY", sessionID, resumeURL, ok)
	}

	second := &scriptedConn{reads: [][]byte{helloFrame(t)}}
	sess.Dial = func(context.Context, string) (Conn, error) { return second, nil }
	_ = sess.oneSocket(ctx, resumeURL, st)
	if ops := opsWritten(t, second.wroteSnapshot()); !firstOpIs(ops, opResume) {
		t.Fatalf("reconnect ops = %v, want Resume (%d) first", ops, opResume)
	}
}

func wantCloseCode(t *testing.T, got []websocket.StatusCode, want websocket.StatusCode) {
	t.Helper()
	if len(got) != 1 || got[0] != want {
		t.Fatalf("close codes = %v, want exactly one %d", got, want)
	}
}

func TestShutdownKeepsTheSessionResumable(t *testing.T) {
	conn := &scriptedConn{reads: [][]byte{helloFrame(t)}}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	sess := Session{Token: "t", Dial: func(context.Context, string) (Conn, error) { return conn, nil }}

	_ = sess.oneSocket(ctx, "ws://x", &resumeState{})

	wantCloseCode(t, conn.closeCodes(), reconnectingClose)
}

type resumeSnapshot struct {
	SessionID string
	ResumeURL string
	OK        bool
}

func matchesResumeState(got, want resumeSnapshot) bool {
	return got.OK && got.SessionID == want.SessionID && got.ResumeURL == want.ResumeURL
}

func TestSessionInvalidSessionNotResumableClearsState(t *testing.T) {
	st := &resumeState{}
	st.ready("sess-1", "ws://resume")
	st.note(intPtr(4))
	sess := Session{Token: "t"}
	if err := sess.onInvalidSession(packet{Op: opInvalidSession, D: mustRaw(t, false)}, st); err == nil {
		t.Fatal("invalid session must end the socket")
	}
	if _, _, ok := st.resumable(); ok {
		t.Fatal("non-resumable invalid session left the session id in place")
	}
}

func TestSessionInvalidSessionResumableKeepsState(t *testing.T) {
	st := &resumeState{}
	st.ready("sess-1", "ws://resume")
	sess := Session{Token: "t"}
	_ = sess.onInvalidSession(packet{Op: opInvalidSession, D: mustRaw(t, true)}, st)
	if _, _, ok := st.resumable(); !ok {
		t.Fatal("resumable invalid session discarded the session id")
	}
}

func TestResumeStateTracksSequence(t *testing.T) {
	st := &resumeState{}
	if st.sequence() != nil {
		t.Fatal("fresh state reported a sequence")
	}
	st.note(intPtr(3))
	st.note(nil)
	got := st.sequence()
	if got == nil || *got != 3 {
		t.Fatalf("sequence = %v, want 3", got)
	}
	st.invalidate()
	if st.sequence() != nil {
		t.Fatal("invalidate left a sequence from the dead session")
	}
}
