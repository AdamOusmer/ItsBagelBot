package main

import (
	"errors"
	"flag"
	"fmt"
	"log"
	"math"
	"os"
	"strings"
	"time"
)

const (
	defaultSLIServices       = "users,commands,modules,loyalty,projector,sesame,gateway,ingress,outgress,transactions,notifications"
	defaultIngressSLISubject = "twitch.ingress.admin.shards.get"
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
	flag.DurationVar(&cfg.runTimeout, "run-timeout", 0, "hard limit for the publish phase (0 disables)")
	flag.DurationVar(&cfg.maxAckGap, "max-ack-gap", 0, "maximum sampled PubAck RTT or publisher completion-progress gap (0 disables)")
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
	flag.BoolVar(&cfg.sliOnly, "sli-only", false, "continuously sample side-effect-free RPC and an isolated Valkey TTL key without using JetStream")
	services := defaultSLIServices
	flag.StringVar(&services, "services", defaultSLIServices, "comma-separated RPC health services sampled in SLI mode")
	flag.DurationVar(&cfg.sliDuration, "duration", 5*time.Minute, "continuous SLI sampling duration")
	flag.DurationVar(&cfg.sliInterval, "interval", 5*time.Second, "delay between continuous SLI samples")
	flag.DurationVar(&cfg.sliTimeout, "timeout", 2*time.Second, "per-operation SLI timeout")
	flag.DurationVar(&cfg.sliMaxRTT, "max-rtt", 250*time.Millisecond, "maximum accepted SLI round-trip time")
	flag.StringVar(&cfg.sliKey, "key", defaultSLIKey(runID), "isolated Valkey SLI key (must start with acceptance:sli:)")
	flag.StringVar(&cfg.sliIngressSubject, "ingress-shards-subject", defaultIngressSLISubject, "read-only ingress shard snapshot subject (empty disables the snapshot SLI)")
	flag.Parse()

	cfg.user = os.Getenv("NATS_USER")
	cfg.password = os.Getenv("NATS_PASSWORD")
	cfg.caFile = os.Getenv("NATS_CA")
	cfg.caPEM = os.Getenv("NATS_CA_PEM")
	cfg.sliServices = parseSLIServices(services)
	cfg.sliNATSURL = firstNonempty(os.Getenv("NATS_RPC_URL"), os.Getenv("NATS_LEAF_URL"), cfg.hubURL)
	cfg.valkeyAddress = os.Getenv("VALKEY_ADDR")
	cfg.valkeyPassword = firstNonempty(os.Getenv("VALKEY_PASSWORD"), os.Getenv("REDISCLI_AUTH"))
	cfg.valkeyCAPEM = os.Getenv("VALKEY_TLS_CA_PEM")

	validateConfig(cfg)
	return cfg
}

func validateConfig(cfg config) {
	validateCredentialMode(cfg)
	if cfg.sliOnly {
		if err := validateSLIConfig(cfg); err != nil {
			log.Fatal(err)
		}
		return
	}
	validatePositiveConfig(cfg)
	validateStreamOptions(cfg)
	validatePublishOptions(cfg)
	validateTopologyOptions(cfg)
}

func validateSLIConfig(cfg config) error {
	if len(cfg.sliServices) == 0 {
		return errors.New("services must include at least one RPC health service")
	}
	seen := make(map[string]struct{}, len(cfg.sliServices))
	for _, service := range cfg.sliServices {
		if service == "" || strings.ContainsAny(service, ".*> \t\r\n") {
			return fmt.Errorf("invalid RPC health service token %q", service)
		}
		if _, duplicate := seen[service]; duplicate {
			return fmt.Errorf("duplicate RPC health service %q", service)
		}
		seen[service] = struct{}{}
	}
	if cfg.sliDuration <= 0 {
		return errors.New("duration must be positive")
	}
	if cfg.sliInterval <= 0 {
		return errors.New("interval must be positive")
	}
	if cfg.sliTimeout <= 0 {
		return errors.New("timeout must be positive")
	}
	if cfg.sliMaxRTT <= 0 {
		return errors.New("max-rtt must be positive")
	}
	if cfg.sliMaxRTT > cfg.sliTimeout {
		return errors.New("max-rtt must not exceed timeout")
	}
	if !strings.HasPrefix(cfg.sliKey, "acceptance:sli:") || len(cfg.sliKey) == len("acceptance:sli:") || strings.ContainsAny(cfg.sliKey, " \t\r\n") {
		return errors.New("key must be an isolated acceptance:sli: key without whitespace")
	}
	if cfg.sliIngressSubject != "" && (strings.ContainsAny(cfg.sliIngressSubject, "*> \t\r\n") || !strings.HasSuffix(cfg.sliIngressSubject, ".get")) {
		return errors.New("ingress-shards-subject must be an exact read-only .get subject or empty")
	}
	if cfg.sliNATSURL == "" {
		return errors.New("NATS_RPC_URL, NATS_LEAF_URL, or hub-url is required")
	}
	if cfg.valkeyAddress == "" {
		return errors.New("VALKEY_ADDR is required in SLI mode")
	}
	if cfg.valkeyPassword == "" {
		return errors.New("VALKEY_PASSWORD or REDISCLI_AUTH is required in SLI mode")
	}
	if !cfg.insecureLocal && cfg.valkeyCAPEM == "" {
		return errors.New("VALKEY_TLS_CA_PEM is required in SLI mode")
	}
	return nil
}

func parseSLIServices(value string) []string {
	parts := strings.Split(value, ",")
	services := make([]string, 0, len(parts))
	for _, part := range parts {
		if service := strings.TrimSpace(part); service != "" {
			services = append(services, service)
		}
	}
	return services
}

func defaultSLIKey(runID string) string {
	host := strings.NewReplacer(" ", "-", ":", "-").Replace(strings.ToLower(hostname()))
	return "acceptance:sli:" + host + ":" + strings.ToLower(runID)
}

func firstNonempty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func validateCredentialMode(cfg config) {
	if cfg.insecureLocal {
		return
	}
	requireCredentials(cfg)
}

func validatePositiveConfig(cfg config) {
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
}

func validateStreamOptions(cfg config) {
	switch cfg.replicas {
	case 1, 3:
	default:
		log.Fatal("replicas must be 1 or 3")
	}
}

func validatePublishOptions(cfg config) {
	switch cfg.mode {
	case "async", "atomic", "fast":
	default:
		log.Fatal("mode must be async, atomic, or fast")
	}
	requireAtMost(cfg.batchSize, 1_000, "batch-size must be <= 1000")
	requireAtMost(cfg.fastOutstanding, 65_535, "fast-outstanding-acks must fit uint16")
	requireFiniteNonNegative(cfg.targetRate, "target-rate must be finite and non-negative")
	requireNonNegative(float64(cfg.latencySamples), "latency-samples must be non-negative")
	requireNonNegative(float64(cfg.runTimeout), "run-timeout must be non-negative")
	requireNonNegative(float64(cfg.maxAckGap), "max-ack-gap must be non-negative")
	requireAtMost(
		cfg.atomicInflight*cfg.publishers,
		50,
		"atomic-inflight x publishers must stay within the server's 50-batch per-stream limit",
	)
}

func validateTopologyOptions(cfg config) {
	requireNonNegative(float64(cfg.topologyDuration), "topology-duration must be non-negative")
	requireEqual(cfg.requiredPeers, cfg.replicas, "required-peers must match replicas")
	requireDifferentWhenSet(
		cfg.preferredLeader,
		cfg.forbiddenLeader,
		"preferred-leader and forbidden-leader must differ",
	)
}

func requireAtMost(value, limit int, message string) {
	if value > limit {
		log.Fatal(message)
	}
}

func requireNonNegative(value float64, message string) {
	if value < 0 {
		log.Fatal(message)
	}
}

func requireFiniteNonNegative(value float64, message string) {
	if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 {
		log.Fatal(message)
	}
}

func requireEqual(value, expected int, message string) {
	if value != expected {
		log.Fatal(message)
	}
}

func requireDifferentWhenSet(value, other, message string) {
	if value == "" {
		return
	}
	if value == other {
		log.Fatal(message)
	}
}

func requireCredentials(cfg config) {
	if cfg.user == "" {
		log.Fatal("NATS_USER is required")
	}
	if cfg.password == "" {
		log.Fatal("NATS_PASSWORD is required")
	}
	if cfg.caFile == "" && cfg.caPEM == "" {
		log.Fatal("NATS_CA or NATS_CA_PEM is required")
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
