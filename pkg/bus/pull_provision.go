// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package bus

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"strconv"
	"strings"
	"time"

	"ItsBagelBot/pkg/env"

	"github.com/nats-io/nats.go"
	jsapi "github.com/nats-io/nats.go/jetstream"
)

const consumerContractMetadata = "bus.contract.version"

func consumerContract(cfg jsapi.ConsumerConfig) int {
	version, _ := strconv.Atoi(cfg.Metadata[consumerContractMetadata])
	return version
}

func newerConsumerContract(live, desired map[string]string) bool {
	return consumerContract(jsapi.ConsumerConfig{Metadata: live}) > consumerContract(jsapi.ConsumerConfig{Metadata: desired})
}

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
	if err := retirePullPredecessor(ctx, js, stream, desired); err != nil {
		return nil, err
	}
	return consumer, deletePullConsumer(ctx, js, stream, desired.Name+"_staging")
}

func deletePullConsumer(ctx context.Context, js pullConsumerProvisioner, stream, name string) error {
	err := js.DeleteConsumer(ctx, stream, name)
	if errors.Is(err, jsapi.ErrConsumerNotFound) {
		return nil
	}
	return err
}

func retirePullPredecessor(ctx context.Context, js pullConsumerProvisioner, stream string, desired jsapi.ConsumerConfig) error {
	info, err := predecessorInfo(ctx, js, stream, strings.TrimSuffix(desired.Name, "_v2"))
	if err != nil || info == nil {
		return err
	}
	_, err = freezePredecessor(ctx, js, info)
	return err
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
	consumer, _, err := provisionPullConsumer(ctx, js, stream, desired)
	return consumer, err
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
		return bindExistingPull(ctx, js, desired, info)
	}
	desired, err = initialPullPosition(ctx, js, stream, desired)
	if err != nil {
		return nil, nil, err
	}
	consumer, err := js.CreateConsumer(ctx, stream, desired)
	if errors.Is(err, jsapi.ErrConsumerExists) {
		return provisionPullConsumer(ctx, js, stream, desired)
	}
	return consumer, info, err
}

func initialPullPosition(ctx context.Context, js pullConsumerProvisioner, stream string, desired jsapi.ConsumerConfig) (jsapi.ConsumerConfig, error) {
	if !strings.HasSuffix(desired.Name, "_v2") {
		return desired, nil
	}
	info, err := pausePredecessor(ctx, js, stream, desired)
	if err != nil {
		return desired, err
	}
	if sameStreamPosition(info, stream) {
		desired.DeliverPolicy = jsapi.DeliverByStartSequencePolicy
		desired.OptStartSeq = info.AckFloor.Stream + 1
	}
	return desired, nil
}

func sameStreamPosition(info *jsapi.ConsumerInfo, stream string) bool {
	if info == nil {
		return false
	}
	return info.Stream == stream && info.AckFloor.Stream > 0
}

func pausePredecessor(ctx context.Context, js pullConsumerProvisioner, stream string, desired jsapi.ConsumerConfig) (*jsapi.ConsumerInfo, error) {
	if !strings.HasSuffix(desired.Name, "_v2") {
		return nil, nil
	}
	legacy := strings.TrimSuffix(desired.Name, "_v2")
	info, err := predecessorInfo(ctx, js, stream, legacy)
	if err != nil || info == nil {
		return info, err
	}
	info, err = drainPredecessor(ctx, js, stream, info)
	if err != nil || info == nil {
		return info, err
	}
	return freezePredecessor(ctx, js, info)
}

func freezePredecessor(ctx context.Context, js pullConsumerProvisioner, info *jsapi.ConsumerInfo) (*jsapi.ConsumerInfo, error) {
	_, err := js.PauseConsumer(ctx, info.Stream, info.Name, time.Now().AddDate(100, 0, 0))
	if errors.Is(err, jsapi.ErrConsumerNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return pullConsumerInfo(ctx, js, info.Stream, info.Name)
}

func drainPredecessor(ctx context.Context, js pullConsumerProvisioner, stream string, info *jsapi.ConsumerInfo) (*jsapi.ConsumerInfo, error) {
	for crossStreamBacklog(info, stream) {
		if !sleepOrDone(ctx, pullLeaderPoll) {
			return nil, ctx.Err()
		}
		next, err := pullConsumerInfo(ctx, js, info.Stream, info.Name)
		if err != nil || next == nil {
			return next, err
		}
		info = next
	}
	return info, nil
}

func crossStreamBacklog(info *jsapi.ConsumerInfo, stream string) bool {
	return info.Stream != stream && info.NumPending > 0
}

func predecessorInfo(ctx context.Context, js pullConsumerProvisioner, stream, legacy string) (*jsapi.ConsumerInfo, error) {
	info, err := pullConsumerInfo(ctx, js, stream, legacy)
	if err != nil || info != nil {
		return info, err
	}
	if stream != TwitchIngressPremiumStream.Name {
		return nil, nil
	}
	info, err = pullConsumerInfo(ctx, js, TwitchIngressStream.Name, legacy)
	if errors.Is(err, jsapi.ErrStreamNotFound) {
		return nil, nil
	}
	return info, err
}

func bindExistingPull(ctx context.Context, js pullConsumerProvisioner, desired jsapi.ConsumerConfig, info *jsapi.ConsumerInfo) (jsapi.Consumer, *jsapi.ConsumerInfo, error) {
	if consumerContract(info.Config) > consumerContract(desired) {
		return nil, info, fmt.Errorf("bus: consumer %q requires newer contract %d", desired.Name, consumerContract(info.Config))
	}
	if pullConsumerConverged(info.Config, desired) {
		consumer, err := js.Consumer(ctx, info.Stream, desired.Name)
		return consumer, info, err
	}
	if !pullConsumerImmutableMatch(info.Config, desired) {
		return nil, info, fmt.Errorf("bus: consumer %q contract drift requires a newer versioned durable", desired.Name)
	}
	desired.DeliverPolicy = info.Config.DeliverPolicy
	desired.OptStartSeq = info.Config.OptStartSeq
	consumer, err := js.CreateOrUpdateConsumer(ctx, info.Stream, desired)
	return consumer, info, err
}

func pullConsumerImmutableMatch(live, desired jsapi.ConsumerConfig) bool {
	type immutable struct {
		ack    jsapi.AckPolicy
		replay jsapi.ReplayPolicy
		memory bool
		push   string
	}
	shape := func(cfg jsapi.ConsumerConfig) immutable {
		return immutable{cfg.AckPolicy, cfg.ReplayPolicy, cfg.MemoryStorage, cfg.DeliverSubject}
	}
	return shape(live) == shape(desired)
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
	contract          int
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
		contract:          consumerContract(cfg),
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

func pullConsumerInfo(ctx context.Context, js pullConsumerProvisioner, stream, name string) (*jsapi.ConsumerInfo, error) {
	consumer, err := js.Consumer(ctx, stream, name)
	if errors.Is(err, jsapi.ErrNotPullConsumer) {
		return pushOccupantInfo(ctx, js, stream, name)
	}
	return lookedUpInfo(ctx, consumer, err, isMissingConsumer)
}

func pushOccupantInfo(ctx context.Context, js pullConsumerProvisioner, stream, name string) (*jsapi.ConsumerInfo, error) {
	occupant, err := js.PushConsumer(ctx, stream, name)
	return lookedUpInfo(ctx, occupant, err, isMissingPushConsumer)
}

func isMissingConsumer(err error) bool {
	return errors.Is(err, jsapi.ErrConsumerNotFound)
}

func isMissingPushConsumer(err error) bool {
	return errors.Is(err, jsapi.ErrConsumerNotFound) || errors.Is(err, jsapi.ErrNotPushConsumer)
}

func lookedUpInfo[C consumerInfoSource](ctx context.Context, consumer C, lookupErr error, missing func(error) bool) (*jsapi.ConsumerInfo, error) {
	if missing(lookupErr) {
		return nil, nil
	}
	if lookupErr != nil {
		return nil, lookupErr
	}
	return infoUnlessDeleted(ctx, consumer)
}

type consumerInfoSource interface {
	Info(ctx context.Context) (*jsapi.ConsumerInfo, error)
}

func infoUnlessDeleted(ctx context.Context, consumer consumerInfoSource) (*jsapi.ConsumerInfo, error) {
	info, err := consumer.Info(ctx)
	if errors.Is(err, jsapi.ErrConsumerNotFound) {
		return nil, nil
	}
	return info, err
}
