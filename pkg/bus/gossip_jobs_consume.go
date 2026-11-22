// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package bus

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"ItsBagelBot/pkg/monitor"

	jsapi "github.com/nats-io/nats.go/jetstream"
	"github.com/newrelic/go-agent/v3/newrelic"
	"go.uber.org/zap"
)

const (
	gossipJobRetryDelay = 2 * time.Second
	gossipJobTimeout    = 3 * time.Minute
	gossipJobFetchBatch = 64
	deliveryAttribute   = "messaging.delivery"
)

var ErrDropJob = errors.New("bus: drop job")

type JobRetryError struct {
	Delay time.Duration
	Err   error
}

func (e JobRetryError) Error() string { return e.Err.Error() }
func (e JobRetryError) Unwrap() error { return e.Err }

type GossipJobMessage struct {
	Kind      GossipJobKind
	UserID    string
	Data      []byte
	Delivered uint64
}

type GossipJobHandler func(context.Context, GossipJobMessage) error

type gossipJobPump struct {
	sub    jsapi.MessagesContext
	handle GossipJobHandler
	log    *zap.Logger
	app    *newrelic.Application
	intake context.Context
}

func (j *GossipJobs) Consume(ctx context.Context, handle GossipJobHandler, log *zap.Logger) (func(), error) {
	sub, err := j.consumer.Messages(jsapi.PullMaxMessages(gossipJobFetchBatch), jsapi.WithMessagesErrOnMissingHeartbeat(false))
	if err != nil {
		return nil, fmt.Errorf("bus: consume gossip jobs: %w", err)
	}
	intake, closeIntake := context.WithCancel(ctx)
	context.AfterFunc(intake, sub.Drain)
	pump := gossipJobPump{sub: sub, handle: handle, log: log, app: monitor.Resolve(nil), intake: intake}
	var running sync.WaitGroup
	loop := make(chan struct{})
	go func() {
		defer close(loop)
		pump.run(context.WithoutCancel(ctx), &running)
	}()
	return func() {
		closeIntake()
		<-loop
		running.Wait()
	}, nil
}

func (p gossipJobPump) run(ctx context.Context, running *sync.WaitGroup) {
	for {
		msg, err := p.sub.Next()
		if err != nil {
			p.subscriptionError(err)
			return
		}
		if p.intake.Err() != nil {
			p.release(msg)
			continue
		}
		running.Go(func() { p.process(ctx, msg) })
	}
}

func (p gossipJobPump) release(msg jsapi.Msg) {
	if err := msg.Nak(); err != nil {
		p.log.Warn("gossip job release failed", zap.Error(err))
	}
}

func (p gossipJobPump) subscriptionError(err error) {
	if p.intake.Err() == nil && !errors.Is(err, jsapi.ErrMsgIteratorClosed) {
		p.log.Warn("gossip job subscription", zap.Error(err))
	}
}

func (p gossipJobPump) process(parent context.Context, msg jsapi.Msg) {
	kind, userID, err := parseGossipJobSubject(msg.Subject())
	if err != nil {
		p.log.Warn("gossip job dropped", zap.String("subject", msg.Subject()), zap.Error(err))
		p.settle(msg.Term, p.log)
		return
	}
	ctx, cancel := context.WithTimeout(parent, gossipJobTimeout)
	defer cancel()
	txn := p.startTransaction(msg)
	defer txn.End()
	log := monitor.TraceLogger(txn, p.log)
	stopProgress := p.trackProgress(ctx, msg)
	err = p.handle(newrelic.NewContext(ctx, txn), GossipJobMessage{Kind: kind, UserID: userID, Data: msg.Data(), Delivered: deliveryCount(msg)})
	stopProgress()
	monitor.NoticeError(txn, err)
	txn.AddAttribute(resultAttribute, messagingResult(err))
	p.conclude(parent, msg, err, log)
}

func (p gossipJobPump) conclude(ctx context.Context, msg jsapi.Msg, err error, log *zap.Logger) {
	var retry JobRetryError
	switch {
	case err == nil:
		p.settle(func() error { return msg.DoubleAck(ctx) }, log)
	case errors.Is(err, ErrDropJob):
		p.settle(msg.Term, log)
	case errors.As(err, &retry) && retry.Delay > 0:
		log.Warn("gossip job rescheduled", zap.String("subject", msg.Subject()), zap.Duration("retry_in", retry.Delay), zap.Error(err))
		p.settle(func() error { return msg.NakWithDelay(retry.Delay) }, log)
	default:
		log.Warn("gossip job failed, retrying", zap.String("subject", msg.Subject()), zap.Uint64("delivery", deliveryCount(msg)), zap.Error(err))
		p.settle(func() error { return msg.NakWithDelay(gossipJobRetryDelay) }, log)
	}
}

func (p gossipJobPump) settle(ack func() error, log *zap.Logger) {
	if err := ack(); err != nil {
		log.Warn("gossip job acknowledgement failed", zap.Error(err))
	}
}

func (p gossipJobPump) startTransaction(msg jsapi.Msg) *newrelic.Transaction {
	txn := p.app.StartTransaction("consume " + GossipJobsStream)
	acceptTraceHeaders(txn, alwaysSampledTrace(msg.Headers()))
	addMessagingTransactionAttributes(txn, messagingAttributes{operation: "process", destination: msg.Subject()})
	txn.AddAttribute(deliveryAttribute, deliveryCount(msg))
	return txn
}

func deliveryCount(msg jsapi.Msg) uint64 {
	meta, err := msg.Metadata()
	if err != nil {
		return 0
	}
	return meta.NumDelivered
}

func (p gossipJobPump) trackProgress(ctx context.Context, msg jsapi.Msg) func() {
	ctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	p.progress(msg)
	go func() {
		defer close(done)
		ticker := time.NewTicker(gossipJobAckWait / 3)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				p.progress(msg)
			}
		}
	}()
	return func() {
		cancel()
		<-done
	}
}

func (p gossipJobPump) progress(msg jsapi.Msg) {
	if err := msg.InProgress(); err != nil {
		p.log.Warn("gossip job progress acknowledgement failed", zap.Error(err))
	}
}
