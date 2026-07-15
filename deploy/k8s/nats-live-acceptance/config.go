package main

import (
	"flag"
	"log"
	"os"
	"strings"
	"time"
)

func parseFlags() config {
	runID := time.Now().UTC().Format("20060102T150405")
	cfg := config{}
	flag.StringVar(&cfg.hubURL, "hub-url", "tls://nats:4222", "direct hub URL")
	flag.StringVar(&cfg.domain, "domain", "hub", "hub JetStream domain")
	flag.StringVar(&cfg.placementTag, "placement-tag", "", "optional server placement tag (normally empty for R3)")
	flag.StringVar(&cfg.stream, "stream", "LIVE_NATS_ACCEPTANCE_"+runID, "temporary stream name")
	flag.StringVar(&cfg.subject, "subject", "twitch.outgress.bench."+strings.ToLower(runID), "isolated benchmark subject")
	flag.IntVar(&cfg.messages, "messages", 200_000, "messages published per endpoint")
	flag.IntVar(&cfg.publishers, "publishers", 2, "independent connections per endpoint")
	flag.IntVar(&cfg.window, "window", 16_384, "maximum outstanding PubAcks per publisher")
	flag.StringVar(&cfg.mode, "mode", "async", "publish mode: async, atomic, or fast")
	flag.IntVar(&cfg.batchSize, "batch-size", 64, "atomic batch size or Fast-Ingest flow value")
	flag.IntVar(&cfg.atomicInflight, "atomic-inflight", 4, "maximum concurrent atomic batches per publisher connection")
	flag.IntVar(&cfg.fastOutstanding, "fast-outstanding-acks", 2, "Fast-Ingest maximum outstanding flow acknowledgements")
	flag.Float64Var(&cfg.targetRate, "target-rate", 0, "aggregate target events/second for this process (0 is unlimited)")
	flag.Int64Var(&cfg.startAtUnixMS, "start-at-unix-ms", 0, "shared UTC start barrier in Unix milliseconds")
	flag.IntVar(&cfg.payloadBytes, "payload-bytes", 256, "payload size")
	flag.IntVar(&cfg.payloadVariants, "payload-variants", 65_536, "pre-generated realistic JSON payload variants")
	flag.IntVar(&cfg.latencySamples, "latency-samples", 500, "PubAck RTT samples collected while load is active")
	flag.DurationVar(&cfg.latencyInterval, "latency-interval", 10*time.Millisecond, "interval between under-load PubAck samples")
	flag.DurationVar(&cfg.ackTimeout, "ack-timeout", 5*time.Second, "maximum wait for one PubAck")
	flag.DurationVar(&cfg.maxP95, "max-p95", 20*time.Millisecond, "maximum accepted under-load PubAck p95")
	flag.DurationVar(&cfg.maxP99, "max-p99", 50*time.Millisecond, "maximum accepted under-load PubAck p99")
	flag.Float64Var(&cfg.minRate, "min-rate", 0, "minimum accepted messages/second per endpoint (0 disables)")
	flag.BoolVar(&cfg.cleanup, "cleanup", true, "delete the temporary stream on exit")
	flag.BoolVar(&cfg.createStream, "create-stream", true, "create the isolated stream before benchmarking")
	flag.BoolVar(&cfg.setupOnly, "setup-only", false, "perform create/cleanup actions without benchmarking")
	flag.IntVar(&cfg.replicas, "replicas", 3, "temporary stream replica count (1 or 3)")
	flag.BoolVar(&cfg.topologyOnly, "topology-only", false, "monitor and validate stream topology without publishing")
	flag.DurationVar(&cfg.topologyDuration, "topology-duration", 0, "duration to monitor topology after the shared start barrier")
	flag.DurationVar(&cfg.topologyInterval, "topology-interval", time.Second, "stream topology polling interval")
	flag.StringVar(&cfg.preferredLeader, "preferred-leader", "", "preferred leader used only when the forbidden server currently leads")
	flag.StringVar(&cfg.forbiddenLeader, "forbidden-leader", "", "server that must remain a current follower")
	flag.IntVar(&cfg.requiredPeers, "required-peers", 3, "required total stream members including the leader")
	flag.DurationVar(&cfg.settleTimeout, "settle-timeout", 30*time.Second, "maximum topology catch-up and leader-move wait")
	flag.StringVar(&cfg.producerID, "producer-id", hostname(), "unique producer label for multi-node runs")
	flag.BoolVar(&cfg.insecureLocal, "insecure-local", false, "allow an open plaintext local test server")
	flag.Parse()

	cfg.user = os.Getenv("NATS_USER")
	cfg.password = os.Getenv("NATS_PASSWORD")
	cfg.caFile = os.Getenv("NATS_CA")

	validateConfig(cfg)
	return cfg
}

func validateConfig(cfg config) {
	if !cfg.insecureLocal {
		requireCredentials(cfg)
	}
	requirePositive(cfg.messages, "messages")
	requirePositive(cfg.publishers, "publishers")
	requirePositive(cfg.window, "window")
	requirePositive(cfg.batchSize, "batch-size")
	requirePositive(cfg.atomicInflight, "atomic-inflight")
	requirePositive(cfg.fastOutstanding, "fast-outstanding-acks")
	requirePositive(cfg.payloadBytes, "payload-bytes")
	requirePositive(cfg.payloadVariants, "payload-variants")
	requirePositive(int(cfg.latencyInterval), "latency-interval")
	requirePositive(int(cfg.ackTimeout), "ack-timeout")
	requirePositive(int(cfg.maxP95), "max-p95")
	requirePositive(int(cfg.maxP99), "max-p99")
	requirePositive(int(cfg.topologyInterval), "topology-interval")
	requirePositive(cfg.requiredPeers, "required-peers")
	requirePositive(int(cfg.settleTimeout), "settle-timeout")
	if cfg.replicas != 1 && cfg.replicas != 3 {
		log.Fatal("replicas must be 1 or 3")
	}
	if cfg.mode != "async" && cfg.mode != "atomic" && cfg.mode != "fast" {
		log.Fatal("mode must be async, atomic, or fast")
	}
	if cfg.batchSize > 1000 {
		log.Fatal("batch-size must be <= 1000")
	}
	if cfg.fastOutstanding > 65_535 {
		log.Fatal("fast-outstanding-acks must fit uint16")
	}
	if cfg.targetRate < 0 {
		log.Fatal("target-rate must be non-negative")
	}
	if cfg.latencySamples < 0 {
		log.Fatal("latency-samples must be non-negative")
	}
	if cfg.topologyDuration < 0 {
		log.Fatal("topology-duration must be non-negative")
	}
	if cfg.requiredPeers != cfg.replicas {
		log.Fatal("required-peers must match replicas")
	}
	if cfg.atomicInflight*cfg.publishers > 50 {
		log.Fatal("atomic-inflight x publishers must stay within the server's 50-batch per-stream limit")
	}
	if cfg.preferredLeader != "" && cfg.preferredLeader == cfg.forbiddenLeader {
		log.Fatal("preferred-leader and forbidden-leader must differ")
	}
}

func requireCredentials(cfg config) {
	if cfg.user == "" {
		log.Fatal("NATS_USER is required")
	}
	if cfg.password == "" {
		log.Fatal("NATS_PASSWORD is required")
	}
	if cfg.caFile == "" {
		log.Fatal("NATS_CA is required")
	}
}

func requirePositive(value int, name string) {
	if value < 1 {
		log.Fatalf("%s must be positive", name)
	}
}

func hostname() string {
	host, err := os.Hostname()
	if err != nil || host == "" {
		return "unknown"
	}
	return host
}
