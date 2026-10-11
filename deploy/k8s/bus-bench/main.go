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

const (
	fallbackURL       = "nats://nats.messaging.svc.cluster.local:4222"
	maxLatencySamples = 12_000_000
	benchPods         = 8
	benchSeqSpan      = 1 << 26
	secSampleCap      = 1 << 16
)

func main() {
	urlDefault := os.Getenv("NATS_URL")
	if urlDefault == "" {
		urlDefault = fallbackURL
	}
	var (
		mode         = flag.String("mode", "", "setup|publish|consume|seat-consumer|cleanup (required)")
		url          = flag.String("url", urlDefault, "")
		subject      = flag.String("subject", "twitch.ingress.retry.benchrig", "")
		group        = flag.String("group", "benchrig", "")
		stream       = flag.String("stream", bus.TwitchIngressRetryStream.Name, "")
		duration     = flag.Duration("duration", 20*time.Second, "")
		startAt      = flag.Int64("start-at", 0, "")
		payloadSize  = flag.Int("payload-size", 256, "")
		idPad        = flag.Int("id-pad", 0, "extra bytes appended to the message id header (header-length sensitivity)")
		confirmEvery = flag.Int("confirm-every", 512, "")
		rate         = flag.Int("rate", 0, "per-pod offered msg/s; 0 = unbounded")
		paceEvery    = flag.Int("pace-every", 64, "messages per pacer sleep; one sleep per message is below timer granularity at high rates")
		pprofPath    = flag.String("pprof", "", "write a CPU profile of a publish or consume run to this path; empty disables")
		pprofAddr    = flag.String("pprof-addr", "", "serve net/http/pprof on this address (e.g. :6061) in publish and consume modes; empty disables")
		feeders      = flag.Int("feeders", 8, "publish goroutines; one distinct publish partition each so every pooled connection gets a worker")
		podIndex     = flag.Int("pod-index", 0, "distinct per publisher pod; seeded into the high bits so sequence identities never collide across pods")
		maxBytes     = flag.Int64("max-bytes", 1<<30, "")
		origMaxBytes = flag.Int64("original-max-bytes", 0, "")
		maxAge       = flag.Duration("max-age", 0, "setup: set the bench stream's MaxAge (and clamp its duplicate window) for the run; 0 leaves it")
		duplicates   = flag.Duration("duplicates", -1, "setup: set the bench stream's duplicate window for the run, clamped to MaxAge; negative leaves it; nats-server replaces 0 with min(2m, MaxAge), so the smallest effective value is a small positive duration")
		msgsPer      = flag.Int64("max-msgs-per", 0, "setup: set the bench stream's MaxMsgsPerSubject for the run; 0 leaves it")
		origMsgsPer  = flag.Int64("original-max-msgs-per", 0, "cleanup: restore this MaxMsgsPerSubject (-1 is unlimited); 0 leaves it")
		origDup      = flag.Duration("original-duplicates", -1, "cleanup: restore this duplicate window, clamped to MaxAge; negative leaves it")
		ackPolicy    = flag.String("ack-policy", "", "consume mode: none|all sets NATS_PULL_ACK_POLICY for the pull lane; empty leaves the environment alone")
		origMaxAge   = flag.Duration("original-max-age", 0, "cleanup: restore this MaxAge")
		preferred    = flag.String("preferred", "", "seat-consumer mode: stream-leader or not-stream-leader; recreates the bench durable until its raft leader lands there")
		routinesMin  = flag.Int("routines-min", 256, "consume mode: weighted lane MinRoutines")
		warmup       = flag.Duration("warmup", 0, "consume mode: exclude this much of the window start from the whole-run percentiles")
		routinesMax  = flag.Int("routines-max", 512, "consume mode: weighted lane MaxRoutines")
	)
	flag.Parse()
	zap.ReplaceGlobals(stderrLogger())

	lane := benchLane{url: *url, stream: *stream, subject: *subject, group: *group}

	stopProfile := startCPUProfile(*pprofPath, *mode)
	startPprofServer(*pprofAddr, *mode)

	modes := map[string]func() error{
		"setup": func() error {
			return runSetup(lane, *maxBytes, newRetentionTarget(0, *msgsPer, *maxAge, *duplicates))
		},
		"publish": func() error {
			return runPublish(publishOpts{
				lane: lane, duration: *duration, startAt: unixNano(*startAt),
				payloadSize: *payloadSize, confirmEvery: *confirmEvery, rate: *rate, idPad: *idPad,
				paceEvery: *paceEvery, podIndex: *podIndex, feeders: *feeders,
			})
		},
		"consume": func() error {
			return runConsumeWithPolicy(*ackPolicy, lane, consumeOptions{duration: *duration, startAt: unixNano(*startAt), payloadSize: *payloadSize, policy: bus.ScalePolicy{MinRoutines: *routinesMin, MaxRoutines: *routinesMax}, warmup: *warmup, feeders: *feeders})
		},
		"seat-consumer": func() error { return runSeatConsumer(lane, *preferred) },
		"cleanup": func() error {
			return runCleanup(lane, newRetentionTarget(*origMaxBytes, *origMsgsPer, *origMaxAge, *origDup))
		},
	}
	err := errors.New("-mode must be setup|publish|consume|seat-consumer|cleanup")
	if run, ok := modes[*mode]; ok {
		err = run()
	}
	stopProfile()
	if err != nil {
		fmt.Fprintln(os.Stderr, "bus-bench:", err)
		os.Exit(1)
	}
}

func newRetentionTarget(maxBytes, msgsPer int64, maxAge, duplicates time.Duration) retentionTarget {
	return retentionTarget{maxBytes: maxBytes, msgsPer: msgsPer, maxAge: maxAge, duplicates: duplicates, hasDuplicates: duplicates >= 0}
}

func runConsumeWithPolicy(policy string, lane benchLane, o consumeOptions) error {
	if err := applyAckPolicy(policy); err != nil {
		return err
	}
	return runConsume(lane, o)
}

func applyAckPolicy(policy string) error {
	switch policy {
	case "":
		return nil
	case "none", "all":
		return os.Setenv("NATS_PULL_ACK_POLICY", policy)
	}
	return fmt.Errorf("-ack-policy must be none|all, got %q", policy)
}

func profiledMode(mode string) bool { return mode == "publish" || mode == "consume" }

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
