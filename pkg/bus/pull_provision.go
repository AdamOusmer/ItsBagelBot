// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package bus

// Provisioning of the pull lane durable: create, bind by lookup when the live
// config already matches, replace the push occupant, and settle on a leader.

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"time"

	"ItsBagelBot/pkg/env"

	"github.com/nats-io/nats.go"
	jsapi "github.com/nats-io/nats.go/jetstream"
)

// ensurePullConsumer provisions the fleet-wide durable and returns a handle to
// fetch from. Every pod runs this against the same name, so all but the first
// are updates.
func ensurePullConsumer(nc *nats.Conn, stream string, desired jsapi.ConsumerConfig) (jsapi.Consumer, error) {
	js, err := jsapi.NewWithDomain(nc, JSDomain())
	if err != nil {
		return nil, fmt.Errorf("bus: modern jetstream context: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), pullProvisionTimeout)
	defer cancel()
	consumer, err := bindPullConsumer(ctx, js, stream, desired)
	if err != nil {
		return nil, err
	}
	awaitPullLeader(ctx, consumer)
	return consumer, nil
}

// awaitPullLeader holds the bind until the durable's group reports the same
// leader on two consecutive reads, or the provisioning budget runs out.
//
// A pull request sent while the consumer group is electing lands on no
// subscriber: between the old leader's teardown and the new leader's setup
// nothing owns the MSG.NEXT subject (nats-server 2.14.4 consumer.go:1676→1781)
// and the waiting queue is discarded (consumer.go:1687-1688), silently. That is
// the window two pods creating the durable in the same instant open for each
// other, and a request lost in it is only re-sent by nats.go at its own
// expiry. On its own this wait is not enough (stagger disabled: 4 of 5
// simultaneous starts still starved); together with pullCreateStagger it was
// the only shape measured clean. A broker without cluster info settles on the
// first read.
func awaitPullLeader(ctx context.Context, consumer jsapi.Consumer) {
	var watch leaderWatch
	for {
		info, err := consumer.Info(ctx)
		if err != nil || info.Cluster == nil {
			return
		}
		if watch.agrees(info.Cluster.Leader) || !sleepOrDone(ctx, pullLeaderPoll) {
			return
		}
	}
}

// leaderWatch counts consecutive INFO reads naming the same leader; agrees
// reports the second such read, which is the settled state ensurePullConsumer
// waits for.
type leaderWatch struct {
	last   string
	agreed int
}

func (w *leaderWatch) agrees(leader string) bool {
	if leader != "" && leader == w.last {
		w.agreed++
	} else {
		w.agreed = 0
	}
	w.last = leader
	return w.agreed >= 2
}

func sleepOrDone(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-ctx.Done():
		return false
	}
}

// bindPullConsumer converges the shared durable, replacing it only when the
// server has refused the update for a field it will never let an update change.
//
// The replacement path exists because the lane consumer's NAME is the same in
// every mode — durableName(group, subject), with no mode token — so flipping
// NATS_CONSUME_MODE to pull re-provisions the durable a push consumer is already
// occupying, and nats-server refuses that conversion in place. Without this the
// flip fails every pod's lane binding, permanently and identically, with the
// lane simply not consuming.
func bindPullConsumer(
	ctx context.Context,
	js pullConsumerProvisioner,
	stream string,
	desired jsapi.ConsumerConfig,
) (jsapi.Consumer, error) {
	consumer, info, err := provisionPullConsumer(ctx, js, stream, desired)
	if err == nil {
		return consumer, nil
	}
	if !requiresConsumerReplacement(err) {
		return nil, fmt.Errorf("bus: provision pull consumer %q: %w", desired.Name, err)
	}

	// Re-drive once against whatever shape the durable has NOW, before anything
	// destructive. This is where the pull path differs from the flow path in
	// kind, not degree: a flow consumer is per-pod, so replacing one costs the
	// caller its own cursor, while this durable is fleet-wide and a delete takes
	// it out from under every other pod fetching from it. A rejection can simply
	// mean another pod completed the conversion between our INFO and our update,
	// and against the successor's shape the same request is an ordinary no-op —
	// so one wasted round trip here is what keeps a simultaneous fleet restart
	// from turning into one delete per pod, each destroying the last one's work.
	if consumer, _, raced := provisionPullConsumer(ctx, js, stream, desired); raced == nil {
		return consumer, nil
	}

	carryLaneAckFloor(&desired, info)
	return replacePullConsumer(js, stream, desired, err)
}

// provisionPullConsumer reads the durable's current shape, echoes the fields an
// update may not change, and issues it. The live info comes back alongside the
// error so the caller can carry the ack floor onto a replacement without a
// second read.
func provisionPullConsumer(
	ctx context.Context,
	js pullConsumerProvisioner,
	stream string,
	desired jsapi.ConsumerConfig,
) (jsapi.Consumer, *jsapi.ConsumerInfo, error) {
	info, err := pullConsumerInfo(ctx, js, stream, desired.Name)
	if err != nil {
		return nil, nil, err
	}
	// The start position is immutable and belongs to whichever pod created the
	// shared durable. Echoing the server's own value keeps every later pod's
	// provision an update; sending DeliverNew at a consumer created elsewhere
	// would be rejected on every boot after the first.
	if info == nil {
		pullCreateStagger()
	}
	if info != nil {
		desired.DeliverPolicy = info.Config.DeliverPolicy
		desired.OptStartSeq = info.Config.OptStartSeq
		if pullConsumerConverged(info.Config, desired) {
			// Bind by lookup. An assignment write against a durable other pods are
			// already fetching from is not free even when it changes nothing — see
			// pullCreateStagger for what a racing write costs the other pods.
			consumer, err := js.Consumer(ctx, stream, desired.Name)
			return consumer, info, err
		}
	}
	consumer, err := js.CreateOrUpdateConsumer(ctx, stream, desired)
	return consumer, info, err
}

// pullConsumerConverged reports whether the live durable already carries every
// field this binding would write. Only the fields pullConsumerConfig sets are
// compared: the server adds metadata and normalises the rest, and a comparison
// that saw those as drift would issue the assignment write on every boot, which
// is exactly the write this check exists to avoid.
// pullConsumerShape is the part of a consumer config this binding writes; two
// configs converge when their shapes are equal, and only then is the durable
// bound by lookup with no assignment write.
type pullConsumerShape struct {
	ackPolicy         jsapi.AckPolicy
	ackWait           time.Duration
	maxAckPending     int
	filterSubject     string
	replicas          int
	memoryStorage     bool
	inactiveThreshold time.Duration
	maxDeliver        int
	replayPolicy      jsapi.ReplayPolicy
	managed           string
}

func shapeOfPullConsumer(cfg jsapi.ConsumerConfig) pullConsumerShape {
	return pullConsumerShape{
		ackPolicy:         cfg.AckPolicy,
		ackWait:           cfg.AckWait,
		maxAckPending:     cfg.MaxAckPending,
		filterSubject:     cfg.FilterSubject,
		replicas:          cfg.Replicas,
		memoryStorage:     cfg.MemoryStorage,
		inactiveThreshold: cfg.InactiveThreshold,
		maxDeliver:        cfg.MaxDeliver,
		replayPolicy:      cfg.ReplayPolicy,
		managed:           cfg.Metadata[managedConsumerMetadata],
	}
}

func pullConsumerConverged(live, desired jsapi.ConsumerConfig) bool {
	return shapeOfPullConsumer(live) == shapeOfPullConsumer(desired)
}

// pullCreateStagger sleeps a random slice of NATS_PULL_CREATE_STAGGER (default
// 2s, 0 disables) before a pod creates a durable that does not exist yet.
//
// Two pods creating the same durable in the same instant is a measured outage
// for one of them (2026-09-01, loopback 3-node nats-server 2.14.4, R3 stream,
// R3 durable): the racing assignment drives a consumer leader/term transition,
// and between the leader's teardown and its re-election nothing subscribes to
// the MSG.NEXT subject (consumer.go:1676→1781) while the waiting queue is
// wiped silently (consumer.go:1687-1688). The loser's first pull requests
// vanish with no status message and no server log, it reports itself healthy,
// and it receives nothing until its own request expiry — 28s at the 30s
// default. A second bind 2s after the first split the lane 50/50 at once,
// which is the number this stagger reproduces. It only ever runs when INFO
// says the durable is absent, so a steady-state boot pays nothing.
//
// The stagger is deliberately NOT followed by a second INFO read that would
// turn the late pod's create into a lookup: measured, that variant starved the
// late pod in 4 of 5 simultaneous starts, against 1 of 3 for the stagger alone
// and 0 of 3 for the stagger together with awaitPullLeader. The create's own
// round trip is part of what settles the durable before the first pull.
func pullCreateStagger() {
	window := env.GetDuration("NATS_PULL_CREATE_STAGGER", 2*time.Second)
	if window <= 0 {
		return
	}
	time.Sleep(time.Duration(rand.Int64N(int64(window))))
}

// replacePullConsumer performs the delete + recreate an immutable-field
// transition requires, on its own budget so the create half cannot inherit an
// already-spent deadline from the update that failed — a delete that succeeds
// followed by a create that times out leaves the lane with no consumer at all.
// The caller has rewritten desired's delivery position, so the recreation
// resumes at the fleet's shared ack floor instead of replaying it.
func replacePullConsumer(
	js pullConsumerProvisioner,
	stream string,
	desired jsapi.ConsumerConfig,
	cause error,
) (jsapi.Consumer, error) {
	ctx, cancel := context.WithTimeout(context.Background(), laneReplaceTimeout)
	defer cancel()

	if err := js.DeleteConsumer(ctx, stream, desired.Name); err != nil &&
		!errors.Is(err, jsapi.ErrConsumerNotFound) {
		return nil, fmt.Errorf(
			"bus: provision pull consumer %q: %w (replace failed: %v)", desired.Name, cause, err)
	}
	consumer, err := js.CreateOrUpdateConsumer(ctx, stream, desired)
	if err != nil {
		return nil, fmt.Errorf("bus: recreate pull consumer %q: %w", desired.Name, err)
	}
	return consumer, nil
}

func pullConsumerInfo(ctx context.Context, js pullConsumerProvisioner, stream, name string) (*jsapi.ConsumerInfo, error) {
	consumer, err := js.Consumer(ctx, stream, name)
	if errors.Is(err, jsapi.ErrConsumerNotFound) {
		return nil, nil
	}
	// jetstream.Consumer refuses to describe a push consumer even for a plain
	// INFO read, and the mode flip by definition finds the push durable
	// occupying this name — the one moment the caller must see the occupant's
	// shape and ack floor to carry them onto the replacement. Read the
	// occupant through the accessor that matches its mode instead of failing
	// the whole flip on the lookup.
	if errors.Is(err, jsapi.ErrNotPullConsumer) {
		return pushOccupantInfo(ctx, js, stream, name)
	}
	if err != nil {
		return nil, err
	}
	info, err := consumer.Info(ctx)
	if errors.Is(err, jsapi.ErrConsumerNotFound) {
		return nil, nil
	}
	return info, err
}

// pushOccupantInfo describes the push consumer currently holding the lane
// durable's name. Finding the name deleted — or already pull — here means
// another pod moved it between the two reads; returning nil info lets the
// provision attempt proceed against the current truth, and bindPullConsumer's
// re-drive absorbs the rejection that follows if the race went the other way.
func pushOccupantInfo(ctx context.Context, js pullConsumerProvisioner, stream, name string) (*jsapi.ConsumerInfo, error) {
	occupant, err := js.PushConsumer(ctx, stream, name)
	if errors.Is(err, jsapi.ErrConsumerNotFound) || errors.Is(err, jsapi.ErrNotPushConsumer) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	info, err := occupant.Info(ctx)
	if errors.Is(err, jsapi.ErrConsumerNotFound) {
		return nil, nil
	}
	return info, err
}
