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

type acceptanceRunID string

type sliServicesFlag string

type fallbackValues []string

type upperBound struct {
	value   int
	limit   int
	failure string
}

type nonNegativeValue struct {
	value   float64
	failure string
}

type integerMatch struct {
	value    int
	expected int
	failure  string
}

type distinctStrings struct {
	value   string
	other   string
	failure string
}

type positiveValue struct {
	value int
	name  string
}

type targetRate float64

func parseFlags() config {
	runID := newAcceptanceRunID()
	cfg := config{}
	flag.StringVar(&cfg.hubURL, "hub-url", "tls://nats:4222", "direct hub URL")
	flag.StringVar(&cfg.domain, "domain", "hub", "hub JetStream domain")
	flag.StringVar(&cfg.placementTag, "placement-tag", "", "optional server placement tag (normally empty for R3)")
	flag.StringVar(&cfg.stream, "stream", runID.streamName(), "temporary stream name")
	flag.StringVar(&cfg.subject, "subject", runID.subjectName(), "isolated benchmark subject")
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
	flag.DurationVar(&cfg.maxP99, "max-p99", 2*time.Millisecond, "maximum accepted under-load PubAck p99")
	flag.Float64Var(&cfg.minRate, "min-rate", 0, "minimum accepted messages/second per endpoint (0 disables)")
	flag.BoolVar(&cfg.cleanup, "cleanup", true, "delete the temporary stream on exit")
	flag.BoolVar(&cfg.createStream, "create-stream", true, "create the isolated stream before benchmarking")
	flag.BoolVar(&cfg.setupOnly, "setup-only", false, "perform create/cleanup actions without benchmarking")
	flag.IntVar(&cfg.replicas, "replicas", 3, "temporary stream replica count (1 or 3)")
	flag.Int64Var(&cfg.maxMsgsPerSubject, "max-msgs-per-subject", 400_000, "rolling per-subject message limit (-1 is unlimited)")
	flag.BoolVar(&cfg.topologyOnly, "topology-only", false, "monitor and validate stream topology without publishing")
	flag.DurationVar(&cfg.topologyDuration, "topology-duration", 0, "duration to monitor topology after the shared start barrier")
	flag.DurationVar(&cfg.topologyInterval, "topology-interval", time.Second, "stream topology polling interval")
	flag.DurationVar(&cfg.topologyGrace, "topology-unhealthy-grace", 0, "bounded grace for a follower to report current again (offline still fails immediately)")
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
	flag.DurationVar(&cfg.sliIngressMaxRTT, "ingress-max-rtt", 500*time.Millisecond, "maximum accepted read-only ingress shard snapshot round-trip time")
	flag.DurationVar(&cfg.sliRPCP99Max, "rpc-p99-max", 8*time.Millisecond, "maximum rolling p99 across node-local RPC health requests")
	flag.IntVar(&cfg.sliRPCP99Min, "rpc-p99-min-samples", 330, "RPC samples required before enforcing the rolling p99 gate")
	flag.StringVar(&cfg.sliKey, "key", runID.defaultSLIKey(), "isolated Valkey SLI key (must start with acceptance:sli:)")
	flag.StringVar(&cfg.sliIngressSubject, "ingress-shards-subject", defaultIngressSLISubject, "read-only ingress shard snapshot subject (empty disables the snapshot SLI)")
	flag.Parse()

	cfg.user = os.Getenv("NATS_USER")
	cfg.password = os.Getenv("NATS_PASSWORD")
	cfg.caFile = os.Getenv("NATS_CA")
	cfg.caPEM = os.Getenv("NATS_CA_PEM")
	cfg.sliServices = sliServicesFlag(services).parse()
	cfg.sliNATSURL = fallbackValues{os.Getenv("NATS_RPC_URL"), os.Getenv("NATS_LEAF_URL"), cfg.hubURL}.firstNonempty()
	cfg.valkeyAddress = os.Getenv("VALKEY_ADDR")
	cfg.valkeyPassword = fallbackValues{os.Getenv("VALKEY_PASSWORD"), os.Getenv("REDISCLI_AUTH")}.firstNonempty()
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
	checks := []func(config) error{
		validateSLIServices,
		validateSLITiming,
		validateSLITailGate,
		validateSLITargets,
	}
	for _, check := range checks {
		if err := check(cfg); err != nil {
			return err
		}
	}
	return nil
}

func validateSLIServices(cfg config) error {
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
	return nil
}

func validateSLITiming(cfg config) error {
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
	if cfg.sliIngressMaxRTT <= 0 {
		return errors.New("ingress-max-rtt must be positive")
	}
	if cfg.sliIngressMaxRTT > cfg.sliTimeout {
		return errors.New("ingress-max-rtt must not exceed timeout")
	}
	return nil
}

func validateSLITailGate(cfg config) error {
	if cfg.sliRPCP99Max <= 0 {
		return errors.New("rpc-p99-max must be positive")
	}
	if cfg.sliRPCP99Max > cfg.sliMaxRTT {
		return errors.New("rpc-p99-max must not exceed max-rtt")
	}
	if cfg.sliRPCP99Min <= 0 || cfg.sliRPCP99Min > sliRPCTailWindowSamples {
		return fmt.Errorf("rpc-p99-min-samples must be between 1 and %d", sliRPCTailWindowSamples)
	}
	return nil
}

func validateSLITargets(cfg config) error {
	checks := []func(config) error{
		validateSLIKey,
		validateSLIIngressSubject,
		validateSLIEndpoints,
	}
	for _, check := range checks {
		if err := check(cfg); err != nil {
			return err
		}
	}
	return nil
}

func validateSLIKey(cfg config) error {
	const prefix = "acceptance:sli:"
	if !strings.HasPrefix(cfg.sliKey, prefix) {
		return errors.New("key must be an isolated acceptance:sli: key without whitespace")
	}
	if strings.TrimPrefix(cfg.sliKey, prefix) == "" {
		return errors.New("key must be an isolated acceptance:sli: key without whitespace")
	}
	if strings.ContainsAny(cfg.sliKey, " \t\r\n") {
		return errors.New("key must be an isolated acceptance:sli: key without whitespace")
	}
	return nil
}

func validateSLIIngressSubject(cfg config) error {
	if cfg.sliIngressSubject == "" {
		return nil
	}
	if strings.ContainsAny(cfg.sliIngressSubject, "*> \t\r\n") || !strings.HasSuffix(cfg.sliIngressSubject, ".get") {
		return errors.New("ingress-shards-subject must be an exact read-only .get subject or empty")
	}
	return nil
}

func validateSLIEndpoints(cfg config) error {
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

func (value sliServicesFlag) parse() []string {
	parts := strings.Split(string(value), ",")
	services := make([]string, 0, len(parts))
	for _, part := range parts {
		if service := strings.TrimSpace(part); service != "" {
			services = append(services, service)
		}
	}
	return services
}

func newAcceptanceRunID() acceptanceRunID {
	return acceptanceRunID(time.Now().UTC().Format("20060102T150405"))
}

func (runID acceptanceRunID) streamName() string {
	return "LIVE_NATS_ACCEPTANCE_" + string(runID)
}

func (runID acceptanceRunID) subjectName() string {
	return "twitch.outgress.bench." + strings.ToLower(string(runID))
}

func (runID acceptanceRunID) defaultSLIKey() string {
	host := strings.NewReplacer(" ", "-", ":", "-").Replace(strings.ToLower(hostname()))
	return "acceptance:sli:" + host + ":" + strings.ToLower(string(runID))
}

func (values fallbackValues) firstNonempty() string {
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
	checks := []positiveValue{
		{cfg.messages, "messages"},
		{cfg.publishers, "publishers"},
		{cfg.window, "window"},
		{cfg.batchSize, "batch-size"},
		{cfg.atomicInflight, "atomic-inflight"},
		{cfg.fastOutstanding, "fast-outstanding-acks"},
		{cfg.payloadBytes, "payload-bytes"},
		{cfg.payloadVariants, "payload-variants"},
		{int(cfg.latencyInterval), "latency-interval"},
		{int(cfg.ackTimeout), "ack-timeout"},
		{int(cfg.maxP95), "max-p95"},
		{int(cfg.maxP99), "max-p99"},
		{int(cfg.topologyInterval), "topology-interval"},
		{cfg.requiredPeers, "required-peers"},
		{int(cfg.settleTimeout), "settle-timeout"},
	}
	for _, check := range checks {
		check.require()
	}
}

func validateStreamOptions(cfg config) {
	switch cfg.replicas {
	case 1, 3:
	default:
		log.Fatal("replicas must be 1 or 3")
	}
	if cfg.maxMsgsPerSubject == 0 || cfg.maxMsgsPerSubject < -1 {
		log.Fatal("max-msgs-per-subject must be positive or -1")
	}
}

func validatePublishOptions(cfg config) {
	switch cfg.mode {
	case "async", "atomic", "fast":
	default:
		log.Fatal("mode must be async, atomic, or fast")
	}
	upperBound{cfg.batchSize, 1_000, "batch-size must be <= 1000"}.require()
	upperBound{cfg.fastOutstanding, 65_535, "fast-outstanding-acks must fit uint16"}.require()
	targetRate(cfg.targetRate).requireValid()
	nonNegativeValue{float64(cfg.latencySamples), "latency-samples must be non-negative"}.require()
	nonNegativeValue{float64(cfg.runTimeout), "run-timeout must be non-negative"}.require()
	nonNegativeValue{float64(cfg.maxAckGap), "max-ack-gap must be non-negative"}.require()
	upperBound{
		cfg.atomicInflight * cfg.publishers,
		50,
		"atomic-inflight x publishers must stay within the server's 50-batch per-stream limit",
	}.require()
}

func validateTopologyOptions(cfg config) {
	nonNegativeValue{float64(cfg.topologyDuration), "topology-duration must be non-negative"}.require()
	nonNegativeValue{float64(cfg.topologyGrace), "topology-unhealthy-grace must be non-negative"}.require()
	integerMatch{cfg.requiredPeers, cfg.replicas, "required-peers must match replicas"}.require()
	distinctStrings{
		cfg.preferredLeader,
		cfg.forbiddenLeader,
		"preferred-leader and forbidden-leader must differ",
	}.requireWhenSet()
}

func (check upperBound) require() {
	if check.value > check.limit {
		log.Fatal(check.failure)
	}
}

func (check nonNegativeValue) require() {
	if check.value < 0 {
		log.Fatal(check.failure)
	}
}

func (rate targetRate) requireValid() {
	const message = "target-rate must be finite and non-negative"
	value := float64(rate)
	if math.IsNaN(value) {
		log.Fatal(message)
	}
	if math.IsInf(value, 0) {
		log.Fatal(message)
	}
	nonNegativeValue{value, message}.require()
}

func (check integerMatch) require() {
	if check.value != check.expected {
		log.Fatal(check.failure)
	}
}

func (check distinctStrings) requireWhenSet() {
	if check.value == "" {
		return
	}
	if check.value == check.other {
		log.Fatal(check.failure)
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

func (check positiveValue) require() {
	if check.value < 1 {
		log.Fatalf("%s must be positive", check.name)
	}
}

func hostname() string {
	host, err := os.Hostname()
	if err != nil || host == "" {
		return "unknown"
	}
	return host
}
