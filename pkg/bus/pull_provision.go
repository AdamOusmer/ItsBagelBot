// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package bus

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

	// Re-drive before deleting: another pod may have just converted this fleet-wide durable.
	if consumer, _, raced := provisionPullConsumer(ctx, js, stream, desired); raced == nil {
		return consumer, nil
	}

	carryLaneAckFloor(&desired, info)
	return replacePullConsumer(js, stream, desired, err)
}

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
	if info == nil {
		pullCreateStagger()
	}
	if info != nil {
		desired.DeliverPolicy = info.Config.DeliverPolicy
		desired.OptStartSeq = info.Config.OptStartSeq
		if pullConsumerConverged(info.Config, desired) {
			consumer, err := js.Consumer(ctx, stream, desired.Name)
			return consumer, info, err
		}
	}
	consumer, err := js.CreateOrUpdateConsumer(ctx, stream, desired)
	return consumer, info, err
}

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

func pullCreateStagger() {
	window := env.GetDuration("NATS_PULL_CREATE_STAGGER", 2*time.Second)
	if window <= 0 {
		return
	}
	time.Sleep(time.Duration(rand.Int64N(int64(window))))
}

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
