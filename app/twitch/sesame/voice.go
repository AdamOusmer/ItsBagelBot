// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package main

import (
	"context"
	"time"

	"ItsBagelBot/app/twitch/sesame/engine"
	"ItsBagelBot/app/twitch/sesame/internal/voice"
	gossiprpc "ItsBagelBot/internal/domain/rpc/gossip"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/svcboot"
)

func startVoice(w wireCtx, deps *engine.Deps) func() {
	if !w.cfg.TTSEnabled {
		return func() {}
	}

	jobsConn, err := bus.ConnectTTSJobs(w.cfg.NATSURL)
	svcboot.FatalIf(w.log, err, "failed to connect TTS job bus")
	overlayConn, err := bus.ConnectTTSOverlay(w.cfg.NATSURL)
	svcboot.FatalIf(w.log, err, "failed to connect TTS overlay bus")

	ctx, cancel := context.WithTimeout(w.ctx, 45*time.Second)
	defer cancel()
	jobs, err := bus.OpenTTSJobs(ctx, jobsConn, bus.TTSReplicas)
	svcboot.FatalIf(w.log, err, "failed to provision TTS job bus")
	overlay, err := bus.OpenTTSOverlay(ctx, overlayConn, bus.TTSReplicas)
	svcboot.FatalIf(w.log, err, "failed to provision TTS overlay bus")

	deps.AlertVoice = voice.Publisher{RPC: w.in.nc, Jobs: jobs}
	worker := voice.Worker{
		RPC: w.in.nc, Gossip: gossiprpc.Subject(w.cfg.GossipRPCPrefix, "tts", "synthesize"), Overlay: overlay,
	}
	stop, err := jobs.Consume(w.ctx, worker.Process, w.log)
	svcboot.FatalIf(w.log, err, "failed to subscribe TTS worker")
	return func() {
		stop()
		overlayConn.Close()
		jobsConn.Close()
	}
}
