// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package main

import (
	"ItsBagelBot/pkg/bus"
	"errors"
	"flag"
	"fmt"
	"go.uber.org/zap"
	"os"
	"runtime/pprof"
	"time"
)

// Command bus-bench drives the fleet's publish and consume paths against a
// real cluster: provision a memory stream, feed it from every pooled
// publisher connection, drain it through one weighted lane, then restore
// what setup changed. Each RIG_REPORT line is JSON the invoking harness parses.

const (
	fallbackURL       = "nats://nats.messaging.svc.cluster.local:4222"
	maxLatencySamples = 12_000_000
	// benchPods bounds the publisher pod indexes the consumer tracks and
	// benchSeqSpan the per-pod sequence range its dedup bitset covers; one
	// second of latency samples is capped at secSampleCap per pod.
	benchPods    = 8
	benchSeqSpan = 1 << 26
	secSampleCap = 1 << 16
)

func main() {
	urlDefault := os.Getenv("NATS_URL")
	if urlDefault == "" {
		urlDefault = fallbackURL
	}
	var (
		mode         = flag.String("mode", "", "setup|publish|consume|cleanup (required)")
		url          = flag.String("url", urlDefault, "")
		subject      = flag.String("subject", "twitch.ingress.retry.benchrig", "")
		group        = flag.String("group", "benchrig", "")
		stream       = flag.String("stream", bus.TwitchIngressRetryStream.Name, "")
		duration     = flag.Duration("duration", 20*time.Second, "")
		startAt      = flag.Int64("start-at", 0, "")
		payloadSize  = flag.Int("payload-size", 256, "")
		confirmEvery = flag.Int("confirm-every", 512, "")
		rate         = flag.Int("rate", 0, "per-pod offered msg/s; 0 = unbounded")
		paceEvery    = flag.Int("pace-every", 64, "messages per pacer sleep; one sleep per message is below timer granularity at high rates")
		pprofPath    = flag.String("pprof", "", "write a CPU profile of a publish or consume run to this path; empty disables")
		feeders      = flag.Int("feeders", 8, "publish goroutines; one distinct publish partition each so every pooled connection gets a worker")
		podIndex     = flag.Int("pod-index", 0, "distinct per publisher pod; seeded into the high bits so sequence identities never collide across pods")
		maxBytes     = flag.Int64("max-bytes", 1<<30, "")
		origMaxBytes = flag.Int64("original-max-bytes", 0, "")
		maxAge       = flag.Duration("max-age", 0, "setup: set the bench stream's MaxAge (and clamp its duplicate window) for the run; 0 leaves it")
		origMaxAge   = flag.Duration("original-max-age", 0, "cleanup: restore this MaxAge")
		routinesMin  = flag.Int("routines-min", 256, "consume mode: weighted lane MinRoutines")
		warmup       = flag.Duration("warmup", 0, "consume mode: exclude this much of the window start from the whole-run percentiles")
		routinesMax  = flag.Int("routines-max", 512, "consume mode: weighted lane MaxRoutines")
	)
	flag.Parse()
	// pkg/bus reports connection-level events (async errors, slow consumer,
	// reconnects) through the global logger; without this they are invisible.
	zap.ReplaceGlobals(stderrLogger())

	// benchLane names the NATS resources one bench run drives: where to dial,
	// which stream and subject carry the traffic, and which consumer group
	// binds it. Every mode addresses the same lane, so they travel together.
	lane := benchLane{url: *url, stream: *stream, subject: *subject, group: *group}

	stopProfile := startCPUProfile(*pprofPath, *mode)

	var err error
	switch *mode {
	case "setup":
		err = runSetup(lane, *maxBytes, *maxAge)
	case "publish":
		err = runPublish(publishOpts{
			lane: lane, duration: *duration, startAt: unixNano(*startAt),
			payloadSize: *payloadSize, confirmEvery: *confirmEvery, rate: *rate,
			paceEvery: *paceEvery, podIndex: *podIndex, feeders: *feeders,
		})
	case "consume":
		err = runConsume(lane, consumeOptions{duration: *duration, startAt: unixNano(*startAt), payloadSize: *payloadSize, policy: bus.ScalePolicy{MinRoutines: *routinesMin, MaxRoutines: *routinesMax}, warmup: *warmup, feeders: *feeders})
	case "cleanup":
		err = runCleanup(lane, *origMaxBytes, *origMaxAge)
	default:
		err = errors.New("-mode must be setup|publish|consume|cleanup")
	}
	stopProfile()
	if err != nil {
		fmt.Fprintln(os.Stderr, "bus-bench:", err)
		os.Exit(1)
	}
}

// profiledMode says which modes measure something worth profiling; setup and
// cleanup are one-shot admin calls whose profile would be noise.
func profiledMode(mode string) bool { return mode == "publish" || mode == "consume" }

// startCPUProfile begins a whole-run CPU profile when -pprof names a path. The
// returned stop is always safe to call — a profile that cannot start degrades
// to a warning on stderr rather than failing the run, since the measurement the
// rig exists for is the report, not the profile.
func startCPUProfile(path, mode string) func() {
	noop := func() {}
	if path == "" || !profiledMode(mode) {
		return noop
	}
	f, err := os.Create(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, "bus-bench: pprof:", err)
		return noop
	}
	if serr := pprof.StartCPUProfile(f); serr != nil {
		fmt.Fprintln(os.Stderr, "bus-bench: pprof:", serr)
		_ = f.Close()
		return noop
	}
	return func() {
		pprof.StopCPUProfile()
		_ = f.Close()
	}
}
