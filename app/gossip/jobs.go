// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package main

import (
	"context"
	"fmt"
	"math/rand/v2"
	"time"

	"ItsBagelBot/app/gossip/internal/config"
	"ItsBagelBot/app/gossip/internal/provider"
	"ItsBagelBot/app/gossip/internal/providers"
	"ItsBagelBot/app/gossip/internal/voice"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/env"

	"github.com/nats-io/nats.go"
	"go.uber.org/zap"
)

const (
	jobsProvisionTimeout = 30 * time.Second
	jobsRetryBase        = 2 * time.Second
	jobsRetryMax         = time.Minute
)

type jobsWiring struct {
	ctx    context.Context
	hubURL string
	cfg    *config.Config
	deps   provider.Deps
	rpc    *nats.Conn
	log    *zap.Logger
}

func startJobs(w jobsWiring) func() {
	if env.Get("GOSSIP_JOBS_BUS_JWT", "") == "" {
		w.log.Info("gossip jobs disabled: GOSSIP_JOBS_BUS_JWT unset")
		return func() {}
	}
	speech, ok := providers.Speech(w.cfg, w.deps)
	if !ok {
		w.log.Warn("gossip jobs disabled: speech provider is not configured")
		return func() {}
	}
	var stopJobs func()
	ctx, cancel := context.WithCancel(w.ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		stopJobs = w.consumeWithRetry(ctx, speech)
	}()
	return func() {
		cancel()
		<-done
		if stopJobs != nil {
			stopJobs()
		}
	}
}

func (w jobsWiring) consumeWithRetry(ctx context.Context, speech voice.Synthesizer) func() {
	for delay := jobsRetryBase; ctx.Err() == nil; delay = min(2*delay, jobsRetryMax/2) {
		stop, err := w.consume(ctx, speech)
		if err == nil {
			return stop
		}
		wait := delay + rand.N(delay)
		w.log.Warn("gossip jobs unavailable, retrying", zap.Error(err), zap.Duration("retry_in", wait))
		select {
		case <-ctx.Done():
		case <-time.After(wait):
		}
	}
	return nil
}

func (w jobsWiring) consume(ctx context.Context, speech voice.Synthesizer) (func(), error) {
	nc, err := bus.ConnectGossipJobs(w.hubURL)
	if err != nil {
		return nil, fmt.Errorf("connect gossip jobs bus: %w", err)
	}
	bindCtx, cancel := context.WithTimeout(ctx, jobsProvisionTimeout)
	defer cancel()
	jobs, err := bus.BindGossipJobs(bindCtx, nc)
	if err != nil {
		nc.Close()
		return nil, fmt.Errorf("bind gossip jobs: %w", err)
	}
	overlays, err := bus.BindOverlays(bindCtx, nc)
	if err != nil {
		nc.Close()
		return nil, fmt.Errorf("bind overlays: %w", err)
	}
	handler := voice.Handler{RPC: w.rpc, Speech: speech, Overlay: overlays, Log: w.log}
	stop, err := jobs.Consume(ctx, handler.Handle, w.log)
	if err != nil {
		nc.Close()
		return nil, fmt.Errorf("consume gossip jobs: %w", err)
	}
	w.log.Info("gossip jobs consuming")
	return func() {
		stop()
		nc.Close()
	}, nil
}
