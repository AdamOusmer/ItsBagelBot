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
	ttsRetryDelay     = 2 * time.Second
	deliveryAttribute = "messaging.delivery"
)

type TTSJobHandler func(context.Context, []byte) error

type TTSRetryError struct {
	Delay time.Duration
	Err   error
}

func (e TTSRetryError) Error() string { return e.Err.Error() }
func (e TTSRetryError) Unwrap() error { return e.Err }

type ttsJobPump struct {
	sub    jsapi.MessagesContext
	handle TTSJobHandler
	log    *zap.Logger
	app    *newrelic.Application
	intake context.Context
}

func (s *TTSJobs) Consume(ctx context.Context, handle TTSJobHandler, workers int, log *zap.Logger) (func(), error) {
	sub, err := s.consumer.Messages(jsapi.PullMaxMessages(workers), jsapi.WithMessagesErrOnMissingHeartbeat(false))
	if err != nil {
		return nil, fmt.Errorf("bus: consume TTS jobs: %w", err)
	}
	intake, closeIntake := context.WithCancel(ctx)
	context.AfterFunc(intake, sub.Drain)
	pump := ttsJobPump{sub: sub, handle: handle, log: log, app: monitor.Resolve(nil), intake: intake}
	handlerCtx := context.WithoutCancel(ctx)
	var pool sync.WaitGroup
	for range workers {
		pool.Go(func() { pump.run(handlerCtx) })
	}
	return func() {
		closeIntake()
		pool.Wait()
	}, nil
}

func (p ttsJobPump) run(ctx context.Context) {
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
		p.process(ctx, msg)
	}
}

func (p ttsJobPump) release(msg jsapi.Msg) {
	if err := msg.Nak(); err != nil {
		p.log.Warn("TTS job release failed", zap.Error(err))
	}
}

func (p ttsJobPump) subscriptionError(err error) {
	if p.intake.Err() == nil && !errors.Is(err, jsapi.ErrMsgIteratorClosed) {
		p.log.Warn("TTS job subscription", zap.Error(err))
	}
}

func (p ttsJobPump) process(parent context.Context, msg jsapi.Msg) {
	ctx, cancel := context.WithTimeout(parent, ttsHandleTimeout)
	defer cancel()
	txn := p.startTransaction(msg)
	defer txn.End()
	log := monitor.TraceLogger(txn, p.log)
	stopProgress := p.trackProgress(ctx, msg)
	err := p.handle(newrelic.NewContext(ctx, txn), msg.Data())
	stopProgress()
	monitor.NoticeError(txn, err)
	txn.AddAttribute(resultAttribute, messagingResult(err))
	if err != nil {
		p.retry(msg, err, log)
		return
	}
	if err := msg.DoubleAck(parent); err != nil {
		log.Warn("TTS job acknowledgement failed", zap.Error(err))
	}
}

func (p ttsJobPump) startTransaction(msg jsapi.Msg) *newrelic.Transaction {
	txn := p.app.StartTransaction("consume " + TTSJobsStream)
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

func (p ttsJobPump) trackProgress(ctx context.Context, msg jsapi.Msg) func() {
	ctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	p.progress(msg)
	go func() {
		defer close(done)
		ticker := time.NewTicker(ttsAckWait / 3)
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

func (p ttsJobPump) progress(msg jsapi.Msg) {
	if err := msg.InProgress(); err != nil {
		p.log.Warn("TTS job progress acknowledgement failed", zap.Error(err))
	}
}

func (p ttsJobPump) retry(msg jsapi.Msg, err error, log *zap.Logger) {
	delay := ttsRetryDelay
	var retry TTSRetryError
	if errors.As(err, &retry) && retry.Delay > 0 {
		delay = retry.Delay
	}
	delivery := deliveryCount(msg)
	fields := []zap.Field{zap.String("subject", msg.Subject()), zap.Uint64("delivery", delivery), zap.Error(err)}
	if delivery >= ttsMaxDeliver {
		log.Error("TTS job abandoned after final delivery", fields...)
	} else {
		log.Warn("TTS job failed, retrying", append(fields, zap.Duration("retry_in", delay))...)
	}
	if err := msg.NakWithDelay(delay); err != nil {
		log.Warn("TTS job retry acknowledgement failed", zap.Error(err))
	}
}
