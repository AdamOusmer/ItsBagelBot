// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"fmt"
	"testing"
	"time"

	"ItsBagelBot/internal/domain/event/data"
	"ItsBagelBot/pkg/bus"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

type outageReporter struct {
	record  func(uint64)
	flush   func(context.Context)
	close   func()
	wake    chan struct{}
	wakeup  *counterReporterWake
	backlog func() counterReporterBacklog
	limit   int
	subject string
	want    counterReporterBacklog
}

func newOutageReporter(r *LoyaltyReporter, uses *useReporter, subject string) outageReporter {
	if subject == data.SubjectCommandUsed {
		uses.done, uses.finished = stoppedReporter()
		return outageReporter{record: func(id uint64) { uses.Record(id, "hug") }, flush: uses.flush, close: uses.Close, wake: uses.wake, wakeup: &uses.wakeup, backlog: uses.backlog, limit: useMaxKeys, want: counterReporterBacklog{uses: useMaxKeys + 1, pending: 1}}
	}
	r.done, r.finished = stoppedReporter()
	record := func(id uint64) { r.BumpChannel(id, "deaths", 1) }
	want := counterReporterBacklog{bumps: loyaltyMaxKeys + 1, pending: 1}
	if subject == data.SubjectLoyaltyEarned {
		record = func(id uint64) { r.Earn(PointEarning{BroadcasterID: 1, ViewerID: id, Points: 1, WatchSeconds: 1}) }
		want = counterReporterBacklog{earn: loyaltyMaxKeys + 1, pending: 1}
		return outageReporter{record: record, flush: r.flush, close: r.Close, wake: r.earnWake, wakeup: &r.earnWakeup, backlog: r.backlog, limit: loyaltyMaxKeys, want: want}
	}
	return outageReporter{record: record, flush: r.flush, close: r.Close, wake: r.wake, wakeup: &r.wakeup, backlog: r.backlog, limit: loyaltyMaxKeys, want: want}
}

func stoppedReporter() (chan struct{}, chan struct{}) {
	finished := make(chan struct{})
	close(finished)
	return make(chan struct{}), finished
}

func TestCounterReportersBackOffDuringOverflow(t *testing.T) {
	for _, subject := range []string{data.SubjectLoyaltyCounters, data.SubjectLoyaltyEarned, data.SubjectCommandUsed} {
		t.Run(subject, func(t *testing.T) { counterReporterOutage(t, subject) })
	}
}

func counterReporterOutage(t *testing.T, subject string) {
	pub := &uncertainCounterPublisher{fail: true}
	core, logs := observer.New(zap.DebugLevel)
	now := time.Unix(1_700_000_000, 0)
	clock := func() time.Time { return now }
	r := retryReporter(pub, zap.New(core), clock)
	r.wake = make(chan struct{}, 1)
	r.earnWake = make(chan struct{}, 1)
	uses := &useReporter{pub: pub, log: zap.New(core), now: clock, pend: map[useKey]int64{}, wake: make(chan struct{}, 1)}
	reporter := newOutageReporter(r, uses, subject)
	reporter.record(1)
	reporter.flush(t.Context())
	original := pub.payloads[subject][0]
	for id := 1; id <= reporter.limit+1; id++ {
		reporter.record(uint64(id))
	}
	require.Empty(t, reporter.wake, "overflow must not nudge a pending reporter")
	for range 100 {
		reporter.wakeup.flush(reporter.flush)
		reporter.flush(t.Context())
	}
	require.Len(t, pub.payloads[subject], 1, "fast failures must not cause a publish loop")
	reporter.subject = subject
	assertReporterRetrySchedule(t, reporter, pub, &now)
	for _, payload := range pub.payloads[subject] {
		require.Equal(t, original, payload)
	}
	assertReporterOutageLogs(t, logs, reporter.want)
	pub.fail = false
	now = now.Add(30 * time.Second)
	reporter.flush(t.Context())
	require.Zero(t, reporter.backlog().pending)
	reporter.record(1)
	pub.fail = true
	reporter.flush(t.Context())
	before := len(pub.payloads[subject])
	now = now.Add(5 * time.Second)
	reporter.flush(t.Context())
	require.Len(t, pub.payloads[subject], before+1, "success resets the next failure to five seconds")
	reporter.close()
	shutdown := logs.FilterMessage("counter reporter work remains unconfirmed at shutdown").All()
	require.Len(t, shutdown, 1)
	require.Equal(t, (counterReporterBacklog{pending: 1}).fields(), shutdown[0].Context)
}

func counterTestReporter(pub bus.Publisher, subject string, clock func() time.Time) outageReporter {
	r := &LoyaltyReporter{pub: pub, log: zap.NewNop(), now: clock, earn: map[earnKey]*earnAgg{}, bumps: map[counterAgg]*bumpAgg{}, wake: make(chan struct{}, 1), earnWake: make(chan struct{}, 1)}
	uses := &useReporter{pub: pub, log: zap.NewNop(), now: clock, pend: map[useKey]int64{}, wake: make(chan struct{}, 1)}
	return newOutageReporter(r, uses, subject)
}

func TestCounterReportersCloseDuringBackoff(t *testing.T) {
	for _, subject := range []string{data.SubjectLoyaltyCounters, data.SubjectLoyaltyEarned, data.SubjectCommandUsed} {
		t.Run(subject, func(t *testing.T) { counterReporterShutdownCases(t, subject) })
	}
}

type shutdownCounterPublisher struct {
	uncertainCounterPublisher
	failures  int
	deadlines []time.Time
}

func (p *shutdownCounterPublisher) PublishOwnedWithID(ctx context.Context, subject, _ string, body []byte) error {
	deadline, _ := ctx.Deadline()
	p.deadlines = append(p.deadlines, deadline)
	p.fail = p.failures != 0
	if p.failures > 0 {
		p.failures--
	}
	return p.PublishOwned(ctx, subject, body)
}

func counterReporterShutdownCases(t *testing.T, subject string) {
	for _, failures := range []int{0, 1, -1} {
		t.Run(fmt.Sprint(failures), func(t *testing.T) { counterReporterShutdown(t, subject, failures) })
	}
}

func counterReporterShutdown(t *testing.T, subject string, failures int) {
	pub := &shutdownCounterPublisher{failures: 1}
	now := time.Unix(1_700_000_000, 0)
	r := counterTestReporter(pub, subject, func() time.Time { return now })
	initial := 2
	if subject == data.SubjectLoyaltyEarned {
		initial = loyaltyChunk + 1
	}
	for id := 1; id <= initial; id++ {
		r.record(uint64(id))
	}
	r.flush(t.Context())
	pending := r.backlog().pending
	require.Equal(t, 2, pending)
	original := pub.payloads[subject][0]
	r.record(99999)
	pub.failures = failures
	started := time.Now()
	r.close()
	bodies := pub.payloads[subject]
	require.Len(t, bodies, pending+2, "shutdown must attempt every pending batch and the aggregate without advancing the clock")
	require.Equal(t, original, bodies[1], "shutdown must preserve the uncertain batch identity and payload")
	want := counterReporterBacklog{}
	if failures < 0 {
		want.pending = pending + 1
	} else {
		want.pending = failures
	}
	require.Equal(t, want, r.backlog(), "failed publications remain pending; all aggregates are drained")
	deadline := pub.deadlines[1]
	require.WithinRange(t, deadline, started, started.Add(5*time.Second+time.Millisecond))
	for _, observed := range pub.deadlines[1:] {
		require.Equal(t, deadline, observed, "all final attempts must share the shutdown budget")
	}
}

func TestLoyaltyReporterNudgesQueuesIndependently(t *testing.T) {
	for _, blocked := range []string{data.SubjectLoyaltyCounters, data.SubjectLoyaltyEarned} {
		t.Run(blocked, func(t *testing.T) {
			pub := &uncertainCounterPublisher{fail: true}
			r := retryReporter(pub, zap.NewNop(), func() time.Time { return time.Unix(1_700_000_000, 0) })
			r.wake, r.earnWake = make(chan struct{}, 1), make(chan struct{}, 1)
			healthy := data.SubjectLoyaltyEarned
			if blocked == healthy {
				healthy = data.SubjectLoyaltyCounters
			}
			backlogged := newOutageReporter(r, nil, blocked)
			active := newOutageReporter(r, nil, healthy)
			backlogged.record(1)
			r.flush(t.Context())
			for id := 1; id <= loyaltyMaxKeys; id++ {
				active.record(uint64(id))
				backlogged.record(uint64(id))
			}
			require.Empty(t, backlogged.wake)
			require.Len(t, active.wake, 1)
			pub.fail = false
			<-active.wake
			active.wakeup.flush(active.flush)
			require.Len(t, pub.payloads[blocked], 1, "a different queue's nudge must respect backoff")
			require.NotEmpty(t, pub.payloads[healthy], "the healthy queue must publish on its overflow nudge")
		})
	}
}

func assertReporterRetrySchedule(t *testing.T, r outageReporter, pub *uncertainCounterPublisher, now *time.Time) {
	t.Helper()
	for attempt, delay := range []time.Duration{5, 10, 20, 30, 30} {
		*now = now.Add((delay - 1) * time.Second)
		r.flush(t.Context())
		require.Len(t, pub.payloads[r.subject], attempt+1)
		*now = now.Add(time.Second)
		r.flush(t.Context())
		require.Len(t, pub.payloads[r.subject], attempt+2)
	}
}

func assertReporterOutageLogs(t *testing.T, logs *observer.ObservedLogs, backlog counterReporterBacklog) {
	t.Helper()
	retained := logs.FilterMessage("loyalty batch retained for retry").All()
	require.Len(t, retained, 2)
	for i, entry := range retained {
		fields := entry.ContextMap()
		require.Equal(t, "*errors.errorString", fields["error_class"])
		require.Equal(t, []time.Duration{0, 65 * time.Second}[i], fields["age"])
	}
	observed := logs.FilterMessage("counter reporter backlog").All()
	require.Len(t, observed, 2)
	require.Equal(t, backlog.fields(), observed[1].Context)
}
