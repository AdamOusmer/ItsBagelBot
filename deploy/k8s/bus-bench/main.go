// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package main

import (
	"ItsBagelBot/pkg/bus"
	"errors"
	"flag"
	"fmt"
	"os"
	"time"
)

// Command bus-bench drives the fleet's publish and consume paths against a
// real cluster: provision a memory stream, feed it from every pooled
// publisher connection, drain it through one weighted lane, then restore
// what setup changed. Each RIG_REPORT line is JSON the invoking harness parses.

const (
	fallbackURL       = "nats://nats.messaging.svc.cluster.local:4222"
	maxLatencySamples = 12_000_000
	dupMapLimit       = 1_000_000
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
		feeders      = flag.Int("feeders", 8, "publish goroutines; one distinct publish partition each so every pooled connection gets a worker")
		podIndex     = flag.Int("pod-index", 0, "distinct per publisher pod; seeded into the high bits so sequence identities never collide across pods")
		maxBytes     = flag.Int64("max-bytes", 1<<30, "")
		origMaxBytes = flag.Int64("original-max-bytes", 0, "")
	)
	flag.Parse()

	// benchLane names the NATS resources one bench run drives: where to dial,
	// which stream and subject carry the traffic, and which consumer group
	// binds it. Every mode addresses the same lane, so they travel together.
	lane := benchLane{url: *url, stream: *stream, subject: *subject, group: *group}

	var err error
	switch *mode {
	case "setup":
		err = runSetup(lane, *maxBytes)
	case "publish":
		err = runPublish(publishOpts{
			lane: lane, duration: *duration, startAt: unixNano(*startAt),
			payloadSize: *payloadSize, confirmEvery: *confirmEvery, rate: *rate,
			podIndex: *podIndex, feeders: *feeders,
		})
	case "consume":
		err = runConsume(lane, *duration, unixNano(*startAt), *payloadSize)
	case "cleanup":
		err = runCleanup(lane, *origMaxBytes)
	default:
		err = errors.New("-mode must be setup|publish|consume|cleanup")
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "bus-bench:", err)
		os.Exit(1)
	}
}
