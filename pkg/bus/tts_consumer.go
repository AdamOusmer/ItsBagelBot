// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package bus

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	jsapi "github.com/nats-io/nats.go/jetstream"
	"go.uber.org/zap"
)

const ttsRetryDelay = 2 * time.Second

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
}

func (s *TTSJobs) Consume(ctx context.Context, handle TTSJobHandler, workers int, log *zap.Logger) (func(), error) {
	sub, err := s.consumer.Messages(jsapi.PullMaxMessages(workers), jsapi.WithMessagesErrOnMissingHeartbeat(false))
	if err != nil {
		return nil, fmt.Errorf("bus: consume TTS jobs: %w", err)
	}
	pump := ttsJobPump{sub: sub, handle: handle, log: log}
	intake, stop := context.WithCancel(ctx)
	var pool sync.WaitGroup
	for range workers {
		pool.Go(func() { pump.run(ctx, intake) })
	}
	return func() {
		stop()
		sub.Stop()
		pool.Wait()
	}, nil
}

func (p ttsJobPump) run(ctx, intake context.Context) {
	for intake.Err() == nil {
		msg, err := p.sub.Next(jsapi.NextContext(intake))
		if err != nil {
			p.subscriptionError(intake, err)
			return
		}
		p.process(ctx, msg)
	}
}

func (p ttsJobPump) subscriptionError(ctx context.Context, err error) {
	if ctx.Err() == nil && !errors.Is(err, jsapi.ErrMsgIteratorClosed) {
		p.log.Warn("TTS job subscription", zap.Error(err))
	}
}

func (p ttsJobPump) process(parent context.Context, msg jsapi.Msg) {
	ctx, cancel := context.WithTimeout(parent, ttsHandleTimeout)
	defer cancel()
	stopProgress := p.trackProgress(ctx, msg)
	err := p.handle(ctx, msg.Data())
	stopProgress()
	if err != nil {
		p.retry(msg, err)
		return
	}
	if err := msg.DoubleAck(parent); err != nil {
		p.log.Warn("TTS job acknowledgement failed", zap.Error(err))
	}
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

func (p ttsJobPump) retry(msg jsapi.Msg, err error) {
	delay := ttsRetryDelay
	var retry TTSRetryError
	if errors.As(err, &retry) && retry.Delay > 0 {
		delay = retry.Delay
	}
	p.log.Warn("TTS job failed", zap.Error(err))
	if err := msg.NakWithDelay(delay); err != nil {
		p.log.Warn("TTS job retry acknowledgement failed", zap.Error(err))
	}
}
