// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package gateway

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	ddiscord "ItsBagelBot/internal/domain/discord"
	"ItsBagelBot/pkg/codec"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var fastLease = leaseTiming{ttl: 200 * time.Millisecond, renew: 10 * time.Millisecond, deadline: 150 * time.Millisecond, poll: 5 * time.Millisecond, release: 100 * time.Millisecond}

type fakeLease struct {
	mu       sync.Mutex
	free     bool
	held     bool
	renewErr error
	acquires int
	releases int
	renews   int
}

func (l *fakeLease) Acquire(context.Context, time.Duration) (bool, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.acquires++
	if l.free && !l.held {
		l.held = true
		return true, nil
	}
	return false, nil
}

func (l *fakeLease) Renew(context.Context, time.Duration) (bool, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.renews++
	return l.held && l.renewErr == nil, l.renewErr
}

func (l *fakeLease) Release(context.Context) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.releases++
	l.held = false
	return nil
}

func (l *fakeLease) setRenewErr(err error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.renewErr = err
}

func (l *fakeLease) revoke() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.held = false
	l.free = false
}

func (l *fakeLease) renewCount() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.renews
}

func (l *fakeLease) counts() (acquires, releases int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.acquires, l.releases
}

type fakeCheckpoint struct {
	mu      sync.Mutex
	stored  Resume
	has     bool
	loadErr error
	saves   []Resume
}

func (c *fakeCheckpoint) Load(context.Context) (Resume, bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.stored, c.has, c.loadErr
}

func (c *fakeCheckpoint) Save(_ context.Context, r Resume) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.saves = append(c.saves, r)
	return nil
}

func (c *fakeCheckpoint) saved() []Resume {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]Resume(nil), c.saves...)
}

type fakeRole struct {
	mu       sync.Mutex
	standing int
	leading  int
}

func (r *fakeRole) Standing(context.Context) {
	r.mu.Lock()
	r.standing++
	r.mu.Unlock()
}

func (r *fakeRole) Leading(context.Context) {
	r.mu.Lock()
	r.leading++
	r.mu.Unlock()
}

type leased struct {
	lease   *fakeLease
	ck      *fakeCheckpoint
	role    *fakeRole
	handler *recHandler
	status  *recStatus
	dial    *dialer
	cancel  context.CancelFunc
	done    chan error
}

func startLeased(t *testing.T, lease *fakeLease, ck *fakeCheckpoint, scripts []script) *leased {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	l := &leased{lease: lease, ck: ck, role: &fakeRole{}, handler: &recHandler{}, status: &recStatus{stop: cancel}, dial: &dialer{scripts: scripts},
		cancel: cancel, done: make(chan error, 1)}
	timing := fastLease
	sess := Session{
		Token: "bot-token", Dial: l.dial.dial, Handle: l.handler, Lease: lease, Checkpoint: ck, Role: l.role, Status: l.status,
		budget: fastBudget(10, nil), timing: &timing,
	}
	go func() { l.done <- sess.Run(ctx) }()
	t.Cleanup(cancel)
	return l
}

func (l *leased) stop(t *testing.T) {
	t.Helper()
	l.cancel()
	select {
	case err := <-l.done:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(5 * time.Second):
		t.Fatal("session did not stop")
	}
}

func (l *leased) dials() int {
	l.dial.mu.Lock()
	defer l.dial.mu.Unlock()
	return len(l.dial.urls)
}

func (l *leased) conn(i int) *scriptedConn {
	l.dial.mu.Lock()
	defer l.dial.mu.Unlock()
	return l.dial.conns[i]
}

func (l *leased) waitSaves(t *testing.T, n int) []Resume {
	t.Helper()
	require.Eventually(t, func() bool { return len(l.ck.saved()) >= n }, 3*time.Second, time.Millisecond)
	return l.ck.saved()
}

func readyScript(f gatewayFrames) []script {
	return []script{{reads: [][]byte{f.hello, f.ready}}}
}

func TestStandbyNeverDialsWhileAnotherReplicaLeads(t *testing.T) {
	l := startLeased(t, &fakeLease{}, &fakeCheckpoint{}, readyScript(newFrames(t)))

	require.Eventually(t, func() bool { a, _ := l.lease.counts(); return a >= 3 }, 3*time.Second, time.Millisecond)
	l.stop(t)

	assert.Zero(t, l.dials())
	assert.Positive(t, l.role.standing)
	assert.Zero(t, l.role.leading)
	assert.Empty(t, l.ck.saved())
}

func TestReadyCheckpointsTheSessionAndShutdownClosesResumablyThenReleases(t *testing.T) {
	f := newFrames(t)
	l := startLeased(t, &fakeLease{free: true}, &fakeCheckpoint{}, readyScript(f))

	first := l.waitSaves(t, 1)
	l.stop(t)

	assert.Equal(t, Resume{SessionID: "sess-1", ResumeURL: "wss://resume", Seq: 4}, first[0])
	assert.Equal(t, []Resume{first[0], first[0]}, l.ck.saved(), "drain saves the final position")
	assert.Equal(t, 1, l.dials())
	wantCloseCode(t, l.conn(0).closeCodes(), reconnectingClose)
	_, releases := l.lease.counts()
	assert.Equal(t, 1, releases)
	assert.Equal(t, 1, l.role.leading)
}

func TestLostLeaseClosesResumablyAndReturnsToStandbyWithoutDialling(t *testing.T) {
	f := newFrames(t)
	l := startLeased(t, &fakeLease{free: true}, &fakeCheckpoint{}, readyScript(f))
	l.waitSaves(t, 1)

	l.lease.revoke()

	require.Eventually(t, func() bool { return len(l.conn(0).closeCodes()) > 0 }, 3*time.Second, time.Millisecond)
	wantCloseCode(t, l.conn(0).closeCodes(), reconnectingClose)
	before, _ := l.lease.counts()
	require.Eventually(t, func() bool { a, _ := l.lease.counts(); return a > before+2 }, 3*time.Second, time.Millisecond)
	l.stop(t)

	assert.Equal(t, 1, l.dials(), "a replica that lost the lease must not dial again")
	assert.Len(t, l.ck.saved(), 1, "a deposed leader must not overwrite its successor's checkpoint")
	_, releases := l.lease.counts()
	assert.Zero(t, releases)
}

func TestRenewErrorsPastTheDeadlineDeposeTheLeader(t *testing.T) {
	lease := &fakeLease{free: true}
	l := startLeased(t, lease, &fakeCheckpoint{}, readyScript(newFrames(t)))
	l.waitSaves(t, 1)

	lease.setRenewErr(errors.New("valkey: connection refused"))

	require.Eventually(t, func() bool { return len(l.conn(0).closeCodes()) > 0 }, 3*time.Second, time.Millisecond)
	l.stop(t)
	wantCloseCode(t, l.conn(0).closeCodes(), reconnectingClose)
}

func TestATransientRenewErrorKeepsTheSocket(t *testing.T) {
	lease := &fakeLease{free: true}
	l := startLeased(t, lease, &fakeCheckpoint{}, readyScript(newFrames(t)))
	l.waitSaves(t, 1)

	lease.setRenewErr(errors.New("valkey: timeout"))
	time.Sleep(30 * time.Millisecond)
	lease.setRenewErr(nil)
	time.Sleep(300 * time.Millisecond)
	l.stop(t)

	assert.Equal(t, 1, l.dials())
	wantCloseCode(t, l.conn(0).closeCodes(), reconnectingClose)
	_, releases := l.lease.counts()
	assert.Equal(t, 1, releases, "the only close is the drain")
	assert.Len(t, l.ck.saved(), 2, "no demotion happened, so the drain saved")
}

func TestStandbyRetakesItsOwnLeaseWithoutWaitingForTheTTL(t *testing.T) {
	l := startLeased(t, &fakeLease{held: true}, &fakeCheckpoint{}, readyScript(newFrames(t)))

	l.waitSaves(t, 1)
	l.stop(t)

	assert.Equal(t, 1, l.dials())
}

func TestFatalCloseParksTheLeaderWhichKeepsTheLease(t *testing.T) {
	scripts := []script{{readErr: errClosed, closeCode: ddiscord.CloseDisallowedIntents}}
	l := startLeased(t, &fakeLease{free: true}, &fakeCheckpoint{}, scripts)
	require.Eventually(t, func() bool {
		l.status.mu.Lock()
		defer l.status.mu.Unlock()
		return len(l.status.downs) > 0
	}, 3*time.Second, time.Millisecond)

	renewed := l.lease.renewCount()
	require.Eventually(t, func() bool { return l.lease.renewCount() > renewed+3 }, 3*time.Second, time.Millisecond)

	select {
	case err := <-l.done:
		t.Fatalf("Run returned %v while parked on a fatal close", err)
	default:
	}
	l.status.mu.Lock()
	assert.True(t, l.status.downs[0].Fatal)
	l.status.mu.Unlock()
	acquires, releases := l.lease.counts()
	assert.Equal(t, 1, l.dials(), "the parked leader must not redial")
	assert.Zero(t, releases)

	l.stop(t)
	_, releases = l.lease.counts()
	assert.Equal(t, 1, releases, "cancel drains normally")
	assert.Equal(t, 1, acquires)
}

func TestSuccessorResumesTheCheckpointedSession(t *testing.T) {
	f := newFrames(t)
	ck := &fakeCheckpoint{has: true, stored: Resume{SessionID: "sess-1", ResumeURL: "wss://resume", Seq: 4}}
	resumed := frame(t, packet{Op: opDispatch, T: eventResumed, S: intPtr(9), D: mustRaw(t, struct{}{})})
	l := startLeased(t, &fakeLease{free: true}, ck, []script{{reads: [][]byte{f.hello, resumed}}})

	saved := l.waitSaves(t, 1)
	l.stop(t)

	assert.Equal(t, Resume{SessionID: "sess-1", ResumeURL: "wss://resume", Seq: 9}, saved[0])
	require.Len(t, l.dial.urls, 1)
	assert.Contains(t, l.dial.urls[0], "wss://resume")
	wrote := l.dial.wrote(0)
	require.NotEmpty(t, wrote)
	var pkt packet
	require.NoError(t, codec.Unmarshal(wrote[0], &pkt))
	assert.Equal(t, opResume, pkt.Op)
	var body struct {
		SessionID string `json:"session_id"`
		Seq       int    `json:"seq"`
	}
	require.NoError(t, codec.Unmarshal(pkt.D, &body))
	assert.Equal(t, "sess-1", body.SessionID)
	assert.Equal(t, 4, body.Seq)
}

func TestMissingOrUnreadableCheckpointIdentifies(t *testing.T) {
	for name, ck := range map[string]*fakeCheckpoint{
		"none":       {},
		"load error": {loadErr: errors.New("valkey: connection refused")},
		"empty":      {has: true, stored: Resume{Seq: 8}},
	} {
		t.Run(name, func(t *testing.T) {
			l := startLeased(t, &fakeLease{free: true}, ck, readyScript(newFrames(t)))

			require.Eventually(t, func() bool { return l.dials() == 1 && len(l.dial.opened(t)) == 1 }, 3*time.Second, time.Millisecond)
			l.stop(t)

			assert.Equal(t, []int{opIdentify}, l.dial.opened(t))
			assert.Equal(t, gatewayURL, l.dial.urls[0])
		})
	}
}

func TestEventsCarryTheirSessionAndSequenceAndCheckpointEveryHundredSeqs(t *testing.T) {
	f := newFrames(t)
	reads := [][]byte{f.hello, f.ready}
	for seq := 5; seq <= 105; seq++ {
		reads = append(reads, frame(t, packet{Op: opDispatch, T: "GUILD_MEMBER_ADD", S: intPtr(seq),
			D: mustRaw(t, map[string]string{"guild_id": "g1"})}))
	}
	l := startLeased(t, &fakeLease{free: true}, &fakeCheckpoint{}, []script{{reads: reads}})

	saves := l.waitSaves(t, 2)
	l.stop(t)

	assert.Equal(t, 104, saves[1].Seq)
	l.handler.mu.Lock()
	defer l.handler.mu.Unlock()
	require.Len(t, l.handler.events, 101)
	assert.Equal(t, Event{Type: "GUILD_MEMBER_ADD", Raw: l.handler.events[0].Raw, SessionID: "sess-1", Seq: 5}, l.handler.events[0])
}
