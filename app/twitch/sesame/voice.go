// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package main

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"sync/atomic"
	"time"

	"ItsBagelBot/app/twitch/sesame/engine"
	"ItsBagelBot/app/twitch/sesame/internal/voice"
	"ItsBagelBot/internal/domain/voiceplan"
	"ItsBagelBot/pkg/bus"

	"go.uber.org/zap"
)

const (
	voiceProvisionTimeout = 45 * time.Second
	voiceRetryBase        = time.Second
	voiceRetryMax         = 30 * time.Second
)

var errVoiceUnavailable = errors.New("TTS bus unavailable")

type pendingVoice struct {
	publisher atomic.Pointer[voice.Publisher]
}

func (p *pendingVoice) Available() bool { return p.publisher.Load() != nil }

func (p *pendingVoice) Enabled(ctx context.Context, id string) (bool, error) {
	if pub := p.publisher.Load(); pub != nil {
		return pub.Enabled(ctx, id)
	}
	return false, nil
}

func (p *pendingVoice) Publish(ctx context.Context, job voiceplan.AlertJob) error {
	pub := p.publisher.Load()
	if pub == nil {
		return errVoiceUnavailable
	}
	return pub.Publish(ctx, job)
}

func startVoice(w wireCtx, deps *engine.Deps) func() {
	if !w.cfg.TTSEnabled {
		return func() {}
	}

	publisher := &pendingVoice{}
	deps.AlertVoice = publisher
	ctx, cancel := context.WithCancel(w.ctx)
	w.ctx = ctx
	shutdown, done := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(done)
		w.provisionVoice(publisher, shutdown)
	}()
	return func() {
		cancel()
		close(shutdown)
		<-done
	}
}

func (w wireCtx) provisionVoice(publisher *pendingVoice, shutdown <-chan struct{}) {
	w.log.Info("provisioning TTS bus in background")
	for delay := voiceRetryBase; w.ctx.Err() == nil; delay = min(2*delay, voiceRetryMax/2) {
		err := w.runVoice(publisher, shutdown)
		if err == nil || w.ctx.Err() != nil {
			return
		}
		wait := delay + rand.N(delay)
		w.log.Warn("TTS bus unavailable, retrying provisioning", zap.Error(err), zap.Duration("retry_in", wait))
		select {
		case <-w.ctx.Done():
			return
		case <-time.After(wait):
		}
	}
}

func (w wireCtx) runVoice(publisher *pendingVoice, shutdown <-chan struct{}) error {
	overlayConn, err := bus.ConnectOverlays(w.cfg.NATSURL)
	if err != nil {
		return fmt.Errorf("connect TTS overlay bus: %w", err)
	}
	defer overlayConn.Close()
	ctx, cancel := context.WithTimeout(w.ctx, voiceProvisionTimeout)
	defer cancel()
	_, err = bus.OpenOverlays(ctx, overlayConn, bus.TTSReplicas)
	if err != nil {
		return fmt.Errorf("provision TTS overlay bus: %w", err)
	}
	jobs, err := bus.OpenGossipJobs(ctx, overlayConn, bus.TTSReplicas)
	if err != nil {
		return fmt.Errorf("provision gossip job bus: %w", err)
	}

	publisher.publisher.Store(&voice.Publisher{RPC: w.in.nc, Jobs: jobs, Log: w.log})
	w.log.Info("TTS bus ready")
	<-shutdown
	return nil
}
