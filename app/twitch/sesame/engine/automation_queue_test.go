// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/internal/domain/automation"
	automationrpc "ItsBagelBot/internal/domain/rpc/automation"
	automationsrpc "ItsBagelBot/internal/domain/rpc/automations"
	"ItsBagelBot/internal/projection"
	"ItsBagelBot/pkg/codec"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/valkey-io/valkey-go"
	"go.uber.org/zap"
)

const (
	queueName     = "line"
	queueLongHold = time.Minute
	queueSettle   = 15 * time.Second
)

var queueTestTiming = queueTiming{
	maxWait:     time.Minute,
	renew:       40 * time.Millisecond,
	pollStep:    2 * time.Millisecond,
	pollMax:     10 * time.Millisecond,
	unavailable: 30 * time.Millisecond,
}

type queueRig struct {
	client   valkey.Client
	store    *ValkeyRunQueue
	bid      uint64
	pub      *fakePublisher
	runs     *autoRunLog
	replicas [2]*Pipeline
	closers  [2]sync.Once
	requests atomic.Int64
}

type queueRigOptions struct {
	lease   time.Duration
	proj    projection.Reader
	limiter RunLimiter
	cool    CooldownStore
	loyalty LoyaltyStore
	mods    func(*AutomationHook) module.Module
	bid     uint64
}

func newQueueRig(t *testing.T, opts queueRigOptions, autos ...automation.Automation) *queueRig {
	t.Helper()
	client := newHotPathTestClient(t)
	rig := &queueRig{client: client, bid: opts.bid, pub: &fakePublisher{}, runs: &autoRunLog{}, store: newValkeyRunQueue(client, opts.lease)}
	if rig.bid == 0 {
		rig.bid = uniqueBroadcaster()
	}
	for i := range rig.replicas {
		rig.replicas[i] = rig.replica(opts, autos)
		rig.replicas[i].queueTiming = queueTestTiming
		t.Cleanup(func() { rig.closeReplica(i) })
	}
	t.Cleanup(rig.wipe)
	return rig
}

func (r *queueRig) replica(opts queueRigOptions, autos []automation.Automation) *Pipeline {
	hook := &AutomationHook{}
	mods := opts.mods
	if mods == nil {
		mods = autoModule
	}
	d := Deps{
		Proj: opts.proj, Live: liveAlways{}, Cooldown: opts.cool, Pub: r.pub, Log: zap.NewNop(), Loyalty: opts.loyalty,
		Automations: AutomationDeps{Source: autoSource{list: autos}, Runs: r.runs, Limiter: opts.limiter, Queue: newValkeyRunQueue(r.client, opts.lease)},
	}
	if d.Proj == nil {
		d.Proj = fakeReader{modules: map[string]projection.ModuleView{AutomationsModuleName: on()}}
	}
	if d.Cooldown == nil {
		d.Cooldown = NoopCooldown{}
	}
	if d.Loyalty == nil {
		d.Loyalty = &autoLoyalty{}
	}
	p := NewPipeline(d, NewRegistry(zap.NewNop(), mods(hook)), Config{OutgressPremium: premiumSubj, OutgressStandard: standardSubj})
	hook.Wire(p)
	return p
}

func (r *queueRig) closeReplica(i int) { r.closers[i].Do(r.replicas[i].Close) }

func (r *queueRig) wipe() {
	keys := runQueueKeys(r.seat("", queueName))
	r.client.Do(context.Background(), r.client.B().Del().Key(keys...).Build())
}

func (r *queueRig) seat(ticket, queue string) RunQueueSeat {
	return RunQueueSeat{BroadcasterID: r.bid, Queue: queue, Ticket: ticket}
}

func (r *queueRig) seatHolder(t *testing.T, ticket string) {
	t.Helper()
	position, err := r.store.Enter(context.Background(), r.seat(ticket, queueName))
	require.NoError(t, err)
	require.Zero(t, position)
}

func (r *queueRig) leave(t *testing.T, ticket string) {
	t.Helper()
	require.NoError(t, r.store.Leave(context.Background(), r.seat(ticket, queueName)))
}

func (r *queueRig) fire(t *testing.T, replica int, name, arg string) string {
	t.Helper()
	reply := r.replicas[replica].RunNamed(context.Background(), r.request(name, arg))
	require.True(t, reply.Accepted, reply.Refusal)
	return reply.RunID
}

func (r *queueRig) request(name, arg string) automationsrpc.RunRequest {
	return automationsrpc.RunRequest{
		BroadcasterID: strconv.FormatUint(r.bid, 10), Name: name, Args: map[string]string{"a": arg},
		RequestID: "req-" + strconv.FormatInt(r.requests.Add(1), 10), Source: automationsrpc.SourceDashboard,
	}
}

func (r *queueRig) line(t *testing.T) []string {
	t.Helper()
	keys := runQueueKeys(r.seat("", queueName))
	out, err := r.client.Do(context.Background(), r.client.B().Zrange().Key(keys[0]).Min("0").Max("-1").Build()).AsStrSlice()
	require.NoError(t, err)
	return out
}

func (r *queueRig) awaitEmptyLine(t *testing.T) {
	t.Helper()
	require.Eventually(t, func() bool { return len(r.line(t)) == 0 }, queueSettle, 5*time.Millisecond)
}

func (r *queueRig) awaitRecord(t *testing.T, runID string) automationrpc.RunRecord {
	t.Helper()
	var found automationrpc.RunRecord
	require.Eventually(t, func() bool {
		for _, rec := range r.runs.records() {
			if rec.RunID == runID {
				found = rec
				return true
			}
		}
		return false
	}, queueSettle, 2*time.Millisecond, "no record for run %s", runID)
	return found
}

func (r *queueRig) texts(t *testing.T) []string {
	t.Helper()
	var out []string
	for _, c := range r.pub.snapshot() {
		for _, m := range flattenMessage(t, c.msg) {
			out = append(out, chatMessageText(t, m))
		}
	}
	return out
}

func (r *queueRig) awaitTexts(t *testing.T, n int) []string {
	t.Helper()
	require.Eventually(t, func() bool { return len(r.pub.snapshot()) >= n }, queueSettle, 2*time.Millisecond)
	return r.texts(t)
}

func queued(id, queue string, actions ...automation.Action) automation.Automation {
	a := manual(id, actions...)
	a.Definition.Queue = queue
	return a
}

func chat(text string) automation.Action { return act("chat", obj{"text": text}) }

func delayMS(ms int) automation.Action { return act("delay", obj{"ms": ms}) }

func runChild(id string) automation.Action { return act("run", obj{"automation_id": id}) }

func TestRunQueueIsFIFOAcrossReplicasUnderConcurrency(t *testing.T) {
	rig := newQueueRig(t, queueRigOptions{lease: queueLongHold},
		queued("tail", queueName, chat("start {args}"), delayMS(3), chat("end {args}")))
	rig.seatHolder(t, "holder")

	const runs = 24
	byRun := map[string]string{}
	var mu sync.Mutex
	var wg sync.WaitGroup
	for i := range runs {
		wg.Go(func() {
			arg := strconv.Itoa(i)
			reply := rig.replicas[i%2].RunNamed(context.Background(), rig.request("tail", arg))
			assert.True(t, reply.Accepted, reply.Refusal)
			mu.Lock()
			byRun[reply.RunID] = arg
			mu.Unlock()
		})
	}
	wg.Wait()

	line := rig.line(t)
	require.Len(t, line, runs+1)
	require.Equal(t, "holder", line[0])
	var want []string
	for _, id := range line[1:] {
		want = append(want, "start "+byRun[id], "end "+byRun[id])
	}
	assert.Empty(t, rig.texts(t), "nothing runs while the holder is in place")
	assert.Zero(t, rig.replicas[0].autoExec.running.Load()+rig.replicas[1].autoExec.running.Load(), "waiters must not spend continuation capacity")

	rig.leave(t, "holder")
	assert.Equal(t, want, rig.awaitTexts(t, len(want)), "runs interleaved or out of order")
	for _, rec := range rig.runs.records() {
		assert.Equal(t, automationrpc.OutcomeOK, rec.Outcome)
	}
	rig.awaitEmptyLine(t)
}

func TestRunQueueSlotOutlivesEveryParallelPath(t *testing.T) {
	path := func(actions ...automation.Action) obj { return obj{"weight": 1, "actions": actions} }
	fork := act("parallel", obj{"branches": []obj{
		path(delayMS(150), chat("late")),
		path(delayMS(10), chat("soon")),
		path(chat("now")),
	}})
	rig := newQueueRig(t, queueRigOptions{lease: queueLongHold}, queued("a", queueName, fork), queued("b", queueName, chat("next")))
	rig.fire(t, 0, "a", "")
	rig.fire(t, 1, "b", "")
	assert.Equal(t, []string{"now", "soon", "late", "next"}, rig.awaitTexts(t, 4))
	rig.awaitEmptyLine(t)
}

func TestRunQueueChildRunsInsideParentSlot(t *testing.T) {
	rig := newQueueRig(t, queueRigOptions{lease: queueLongHold},
		queued("parent", queueName, delayMS(80), runChild("child"), chat("parent done")),
		queued("child", queueName, delayMS(80), chat("child done")),
		queued("next", queueName, chat("next")))
	rig.fire(t, 0, "parent", "")
	rig.fire(t, 1, "next", "")
	assert.Equal(t, []string{"parent done", "child done", "next"}, rig.awaitTexts(t, 3), "the child jumped out of its parent's slot")
	rig.awaitEmptyLine(t)
}

func TestRunQueueTwoRecipesCallingEachOthersQueueDoNotDeadlock(t *testing.T) {
	rig := newQueueRig(t, queueRigOptions{lease: queueLongHold},
		queued("left", "left_line", delayMS(5), runChild("right"), chat("left")),
		queued("right", "right_line", delayMS(5), runChild("left"), chat("right")))
	var ids []string
	for range 6 {
		ids = append(ids, rig.fire(t, 0, "left", ""), rig.fire(t, 1, "right", ""))
	}
	for _, id := range ids {
		rec := rig.awaitRecord(t, id)
		assert.NotEqual(t, queueReasonTimeout, rec.Reason)
	}
}

func TestRunQueueFullRefusesAndCloseCancelsWaitersWithoutHanging(t *testing.T) {
	rig := newQueueRig(t, queueRigOptions{lease: queueLongHold}, queued("tail", queueName, chat("ran")))
	rig.seatHolder(t, "holder")

	waiters := make([]string, 0, maxQueueWaiting)
	for i := range maxQueueWaiting {
		waiters = append(waiters, rig.fire(t, i%2, "tail", ""))
	}
	refused := rig.awaitRecord(t, rig.fire(t, 0, "tail", ""))
	assert.Equal(t, automationrpc.OutcomeLimited, refused.Outcome)
	assert.Equal(t, queueReasonFull, refused.Reason)
	assert.Len(t, rig.line(t), maxQueueWaiting+1)

	started := time.Now()
	rig.closeReplica(0)
	rig.closeReplica(1)
	assert.Less(t, time.Since(started), runQueueCloseTimeout, "close must not wait out the queue")
	for _, id := range waiters {
		rec := rig.awaitRecord(t, id)
		assert.Equal(t, automationrpc.OutcomeFailed, rec.Outcome)
		assert.Equal(t, queueReasonCanceled, rec.Reason)
	}
	assert.Equal(t, []string{"holder"}, rig.line(t))
	assert.Empty(t, rig.texts(t))
}

func TestRunQueueNodeWaiterBoundRefusesAndFreesTheSeat(t *testing.T) {
	rig := newQueueRig(t, queueRigOptions{lease: queueLongHold}, queued("tail", queueName, chat("ran")))
	rig.seatHolder(t, "holder")
	rig.replicas[0].queueWaiters.limit = 0
	rec := rig.awaitRecord(t, rig.fire(t, 0, "tail", ""))
	assert.Equal(t, automationrpc.OutcomeLimited, rec.Outcome)
	assert.Equal(t, queueReasonFull, rec.Reason)
	assert.Equal(t, []string{"holder"}, rig.line(t))
}

func TestRunQueueTimedOutWaiterGivesUpItsPlace(t *testing.T) {
	rig := newQueueRig(t, queueRigOptions{lease: queueLongHold}, queued("tail", queueName, chat("{args}")))
	rig.seatHolder(t, "holder")
	rig.replicas[0].queueTiming.maxWait = 80 * time.Millisecond

	gaveUp := rig.fire(t, 0, "tail", "gave up")
	stayed := rig.fire(t, 1, "tail", "stayed")
	rec := rig.awaitRecord(t, gaveUp)
	assert.Equal(t, automationrpc.OutcomeLimited, rec.Outcome)
	assert.Equal(t, queueReasonTimeout, rec.Reason)
	assert.Equal(t, []string{"holder", stayed}, rig.line(t))

	rig.leave(t, "holder")
	assert.Equal(t, automationrpc.OutcomeOK, rig.awaitRecord(t, stayed).Outcome)
	assert.Equal(t, []string{"stayed"}, rig.texts(t))
}

func TestRunQueueCrashedHolderAndWaiterExpire(t *testing.T) {
	const lease = 250 * time.Millisecond
	rig := newQueueRig(t, queueRigOptions{lease: lease}, queued("tail", queueName, chat("ran")))
	rig.seatHolder(t, "crashed holder")
	position, err := rig.store.Enter(context.Background(), rig.seat("crashed waiter", queueName))
	require.NoError(t, err)
	require.EqualValues(t, 1, position)
	for _, key := range runQueueKeys(rig.seat("", queueName)) {
		pttl, err := rig.client.Do(context.Background(), rig.client.B().Pttl().Key(key).Build()).AsInt64()
		require.NoError(t, err)
		assert.Positive(t, pttl, "an abandoned queue must not live forever")
	}

	started := time.Now()
	rec := rig.awaitRecord(t, rig.fire(t, 0, "tail", ""))
	assert.Equal(t, automationrpc.OutcomeOK, rec.Outcome)
	assert.GreaterOrEqual(t, time.Since(started), lease/2)
	rig.awaitEmptyLine(t)
}

func TestRunQueueLeaseRenewsThroughALongHold(t *testing.T) {
	const lease = 120 * time.Millisecond
	rig := newQueueRig(t, queueRigOptions{lease: lease},
		queued("long", queueName, delayMS(int(4*lease/time.Millisecond)), chat("long")),
		queued("next", queueName, chat("next")))
	rig.fire(t, 0, "long", "")
	rig.fire(t, 1, "next", "")
	assert.Equal(t, []string{"long", "next"}, rig.awaitTexts(t, 2))
}

func TestRunQueueWaiterSurvivesPooledState(t *testing.T) {
	first := command("!first", map[string]any{})
	first.Definition.Queue = queueName
	first.Definition.Actions = []automation.Action{chat("first {user} {args}"), delayMS(1), chat("second {user} {args}")}
	other := command("!other", map[string]any{})
	other.Definition.Actions = []automation.Action{chat("other {user} {args}")}
	rig := newQueueRig(t, queueRigOptions{lease: queueLongHold, bid: 123}, first, other)
	rig.seatHolder(t, "holder")

	process(t, rig.replicas[0], chatEnv("!first alpha beta", ""))
	bob := chatEnv("hello there", "")
	bob.ChatterUserID, bob.ChatterUserLogin = "555", "bob"
	for _, text := range []string{"hello there", "!other zzz", "hello again", "!other yyy"} {
		bob.Text = text
		process(t, rig.replicas[0], bob)
	}
	require.Len(t, rig.line(t), 2, "the handler returned with the run still waiting")
	assert.Equal(t, []string{"other bob zzz", "other bob yyy"}, rig.texts(t))

	rig.leave(t, "holder")
	assert.Equal(t, []string{"other bob zzz", "other bob yyy", "first alice alpha beta", "second alice alpha beta"}, rig.awaitTexts(t, 4))
	rig.awaitEmptyLine(t)
}

func TestRunQueueWaiterKeepsTheViewersRole(t *testing.T) {
	gated := command("!gated", map[string]any{})
	gated.Definition.Queue = queueName
	gated.Definition.Actions = []automation.Action{act("if", obj{
		"condition": obj{"type": "perm", "params": obj{"level": "mod"}},
		"then":      []automation.Action{chat("mod path")},
		"else":      []automation.Action{chat("viewer path")},
	})}
	rig := newQueueRig(t, queueRigOptions{lease: queueLongHold, bid: 123}, gated)
	rig.seatHolder(t, "holder")
	process(t, rig.replicas[0], chatEnv("!gated", "moderator"))
	rig.leave(t, "holder")
	assert.Equal(t, []string{"mod path"}, rig.awaitTexts(t, 1))
}

type downQueue struct {
	enterErr error
	touchErr error
}

func (q downQueue) Enter(context.Context, RunQueueSeat) (int64, error) { return 1, q.enterErr }

func (q downQueue) Touch(context.Context, RunQueueSeat) (int64, error) { return 1, q.touchErr }

func (q downQueue) Leave(context.Context, RunQueueSeat) error { return nil }

func TestRunQueueValkeyDownRunsUnqueued(t *testing.T) {
	boom := errors.New("ERR valkey down")
	for name, queue := range map[string]downQueue{"on enter": {enterErr: boom}, "while waiting": {touchErr: boom}} {
		t.Run(name, func(t *testing.T) {
			fx := newAutoFixture(queued("tail", queueName, chat("ran")))
			fx.p.automations.Queue = queue
			fx.p.queueTiming = queueTestTiming
			rec := fx.run(t, "tail", "req-down", nil)
			assert.Equal(t, automationrpc.OutcomeOK, rec.Outcome)
			assert.Len(t, fx.messages(t), 1)
		})
	}
}

func TestRunQueueReseatsAWaiterWhoseSeatWasLost(t *testing.T) {
	rig := newQueueRig(t, queueRigOptions{lease: queueLongHold}, queued("tail", queueName, chat("ran")))
	rig.seatHolder(t, "holder")
	id := rig.fire(t, 0, "tail", "")
	rig.wipe()
	assert.Equal(t, automationrpc.OutcomeOK, rig.awaitRecord(t, id).Outcome)
	rig.awaitEmptyLine(t)
}

func TestRunQueueReleasesWhenPremiumLapses(t *testing.T) {
	reader := &premiumReader{fakeReader: fakeReader{modules: map[string]projection.ModuleView{AutomationsModuleName: on()}}}
	reader.premium.Store(true)
	rig := newQueueRig(t, queueRigOptions{lease: queueLongHold, proj: reader, mods: betaAutoModule},
		queued("paused", queueName, delayMS(40), chat("late")),
		queued("tail", queueName, chat("ran")))

	paused := rig.fire(t, 0, "paused", "")
	reader.premium.Store(false)
	rec := rig.awaitRecord(t, paused)
	assert.Equal(t, "premium_required", rec.Reason)
	rig.awaitEmptyLine(t)

	reader.premium.Store(true)
	rig.seatHolder(t, "holder")
	waiting := rig.fire(t, 1, "tail", "")
	reader.premium.Store(false)
	rig.leave(t, "holder")
	rec = rig.awaitRecord(t, waiting)
	assert.Equal(t, automationrpc.OutcomeLimited, rec.Outcome)
	assert.Equal(t, "premium_required", rec.Reason)
	rig.awaitEmptyLine(t)
	assert.Empty(t, rig.texts(t))
}

type panickingLoyalty struct{ autoLoyalty }

func (*panickingLoyalty) CounterSet(context.Context, CounterAssignment) (bool, error) {
	panic("store exploded")
}

func TestRunQueueReleasesAfterAPanic(t *testing.T) {
	rig := newQueueRig(t, queueRigOptions{lease: queueLongHold, loyalty: &panickingLoyalty{}},
		queued("boom", queueName, act("counter_set", obj{"name": "last", "value": 7})))
	assert.Panics(t, func() { rig.replicas[0].RunNamed(context.Background(), rig.request("boom", "")) })
	rig.awaitEmptyLine(t)
}

func TestRunQueueReleasesOnEveryEnding(t *testing.T) {
	cases := map[string]struct {
		actions  []automation.Action
		capacity bool
	}{
		"stop":         {actions: []automation.Action{act("stop", obj{}), chat("never")}},
		"failed step":  {actions: []automation.Action{runChild("missing")}},
		"no capacity":  {actions: []automation.Action{delayMS(1), chat("limited")}, capacity: true},
		"plain finish": {actions: []automation.Action{chat("done")}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			rig := newQueueRig(t, queueRigOptions{lease: queueLongHold}, queued("a", queueName, tc.actions...))
			if tc.capacity {
				rig.replicas[0].autoExec.running.Store(maxContinuations)
			}
			rig.awaitRecord(t, rig.fire(t, 0, "a", ""))
			rig.awaitEmptyLine(t)
		})
	}
}

type countingLimiter struct {
	openLimiter
	allowed atomic.Int64
}

func (l *countingLimiter) AllowRun(context.Context, uint64) (bool, error) {
	l.allowed.Add(1)
	return true, nil
}

type firstOnlyCooldown struct {
	NoopCooldown
	claims atomic.Int64
}

func (c *firstOnlyCooldown) Allow(context.Context, string, time.Duration) (bool, error) {
	return c.claims.Add(1) == 1, nil
}

func TestRunQueueChecksConditionsAndRateOnceBeforeQueuing(t *testing.T) {
	limiter, cooldown := &countingLimiter{}, &firstOnlyCooldown{}
	tail := queued("tail", queueName, chat("ran"))
	raw, err := codec.Marshal(obj{"global_s": 60})
	require.NoError(t, err)
	tail.Definition.Conditions = []automation.Condition{{Type: "cooldown", Params: raw}}
	rig := newQueueRig(t, queueRigOptions{lease: queueLongHold, limiter: limiter, cool: cooldown}, tail)
	rig.seatHolder(t, "holder")

	waiting := rig.fire(t, 0, "tail", "")
	assert.Equal(t, []string{"holder", waiting}, rig.line(t))

	refused := rig.awaitRecord(t, rig.fire(t, 1, "tail", ""))
	assert.Equal(t, automationrpc.OutcomeSkipped, refused.Outcome)
	assert.Equal(t, []string{"holder", waiting}, rig.line(t), "a run its conditions refused takes no place")

	rig.leave(t, "holder")
	assert.Equal(t, automationrpc.OutcomeOK, rig.awaitRecord(t, waiting).Outcome)
	assert.EqualValues(t, 2, limiter.allowed.Load())
	assert.EqualValues(t, 2, cooldown.claims.Load())
}

func TestRunQueueIgnoredWithoutAQueueName(t *testing.T) {
	rig := newQueueRig(t, queueRigOptions{lease: queueLongHold}, manual("free", chat("ran")))
	rig.seatHolder(t, "holder")
	assert.Equal(t, automationrpc.OutcomeOK, rig.awaitRecord(t, rig.fire(t, 0, "free", "")).Outcome)
	assert.Equal(t, []string{"holder"}, rig.line(t))
}
