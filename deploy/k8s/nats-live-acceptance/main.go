// Command nats-live-acceptance measures the production JetStream PubAck path
// directly through the TLS hub. Leaves are RPC-only. It creates an isolated, memory-backed
// stream on a unique subject and removes it before exiting, so no production
// consumer can observe benchmark payloads.
package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/nats-io/nats.go"
	jsapi "github.com/nats-io/nats.go/jetstream"
)

type config struct {
	hubURL           string
	domain           string
	placementTag     string
	stream           string
	subject          string
	messages         int
	publishers       int
	window           int
	mode             string
	batchSize        int
	atomicInflight   int
	fastOutstanding  int
	targetRate       float64
	startAtUnixMS    int64
	payloadBytes     int
	payloadVariants  int
	latencySamples   int
	latencyInterval  time.Duration
	ackTimeout       time.Duration
	maxP95           time.Duration
	maxP99           time.Duration
	minRate          float64
	cleanup          bool
	createStream     bool
	setupOnly        bool
	replicas         int
	topologyOnly     bool
	topologyDuration time.Duration
	topologyInterval time.Duration
	preferredLeader  string
	forbiddenLeader  string
	requiredPeers    int
	settleTimeout    time.Duration
	producerID       string
	insecureLocal    bool
	user             string
	password         string
	caFile           string
}

type endpoint struct {
	label  string
	url    string
	domain string
}

type result struct {
	Endpoint        string  `json:"endpoint"`
	Producer        string  `json:"producer"`
	Mode            string  `json:"mode"`
	Server          string  `json:"server"`
	TLSVersion      string  `json:"tls_version"`
	TLSCipher       string  `json:"tls_cipher"`
	Messages        int     `json:"messages"`
	Replicas        int     `json:"replicas"`
	BatchSize       int     `json:"batch_size"`
	AtomicInflight  int     `json:"atomic_inflight"`
	FastOutstanding int     `json:"fast_outstanding_acks"`
	PayloadProfile  string  `json:"payload_profile"`
	LatencyProfile  string  `json:"latency_profile"`
	Acknowledged    int64   `json:"acknowledged"`
	Errors          int64   `json:"errors"`
	DurationMS      int64   `json:"duration_ms"`
	StartedUnixMS   int64   `json:"started_unix_ms"`
	FinishedUnixMS  int64   `json:"finished_unix_ms"`
	MessagesPerSec  float64 `json:"messages_per_second"`
	MiBPerSec       float64 `json:"mib_per_second"`
	PubAckMinMS     float64 `json:"puback_min_ms"`
	PubAckP50MS     float64 `json:"puback_p50_ms"`
	PubAckP95MS     float64 `json:"puback_p95_ms"`
	PubAckP99MS     float64 `json:"puback_p99_ms"`
	PubAckMaxMS     float64 `json:"puback_max_ms"`
	LatencySamples  int     `json:"latency_samples"`
	LatencyRequired int     `json:"latency_required"`
	Reconnects      int64   `json:"reconnects"`
	Disconnects     int64   `json:"disconnects"`
	AsyncErrors     int64   `json:"async_errors"`
	Timeouts        int64   `json:"timeouts"`
	Passed          bool    `json:"passed"`
	Failure         string  `json:"failure,omitempty"`
}

type client struct {
	nc     *nats.Conn
	js     nats.JetStreamContext
	modern jsapi.JetStream
	stats  *connectionStats
}

type connectionStats struct {
	reconnects  atomic.Int64
	disconnects atomic.Int64
	asyncErrors atomic.Int64
}

type acceptanceRun struct {
	cfg       config
	hub       endpoint
	tlsConfig *tls.Config
	setup     client
}

func main() {
	if err := run(); err != nil {
		log.Print(err)
		os.Exit(1)
	}
}

func run() (runErr error) {
	execution, err := newAcceptanceRun()
	if err != nil {
		return err
	}
	defer execution.close(&runErr)
	return execution.execute()
}

func newAcceptanceRun() (*acceptanceRun, error) {
	cfg := parseFlags()
	tlsConfig, err := clientTLS(cfg.caFile)
	if err != nil {
		return nil, err
	}
	hub := endpoint{label: "hub", url: cfg.hubURL, domain: cfg.domain}
	setup, err := prepareStream(cfg, hub, tlsConfig)
	if err != nil {
		return nil, err
	}
	return &acceptanceRun{cfg: cfg, hub: hub, tlsConfig: tlsConfig, setup: setup}, nil
}

func (r *acceptanceRun) close(runErr *error) {
	*runErr = errors.Join(*runErr, closeSetup(r.cfg, r.setup))
}

func (r *acceptanceRun) execute() error {
	if r.cfg.topologyOnly {
		return r.executeTopology()
	}
	if r.cfg.setupOnly {
		return r.executeSetup()
	}
	return r.executeBenchmark()
}

func (r *acceptanceRun) executeTopology() error {
	report, err := monitorStreamTopology(r.cfg, r.setup)
	printTopologyReport(r.cfg, report)
	return err
}

func (r *acceptanceRun) executeSetup() error {
	fmt.Printf(
		"{\"stream\":%q,\"subject\":%q,\"created\":%t,\"deleted\":%t}\n",
		r.cfg.stream,
		r.cfg.subject,
		r.cfg.createStream,
		r.cfg.cleanup,
	)
	return nil
}

func (r *acceptanceRun) executeBenchmark() error {
	benchmarkResult, benchmarkErr := r.collectBenchmarkResult()
	if err := printBenchmarkResult(r.cfg, benchmarkResult); err != nil {
		return err
	}
	return benchmarkGateError(benchmarkErr)
}

func (r *acceptanceRun) collectBenchmarkResult() (result, error) {
	benchmarkResult, benchmarkErr := benchmark(r.cfg, r.hub, r.tlsConfig)
	benchmarkResult.Passed = benchmarkErr == nil
	if benchmarkErr != nil {
		log.Printf("hub benchmark failed: %v", benchmarkErr)
		benchmarkResult.Failure = benchmarkErr.Error()
	}
	return benchmarkResult, benchmarkErr
}

func printBenchmarkResult(cfg config, benchmarkResult result) error {
	out, err := json.MarshalIndent(map[string]any{
		"stream":  cfg.stream,
		"subject": cfg.subject,
		"results": []result{benchmarkResult},
	}, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(out))
	return nil
}

func benchmarkGateError(benchmarkErr error) error {
	if benchmarkErr != nil {
		return errors.New("one or more NATS acceptance gates failed")
	}
	return nil
}

func prepareStream(cfg config, hub endpoint, tlsConfig *tls.Config) (client, error) {
	if err := validateTemporaryTarget(cfg.stream, cfg.subject); err != nil {
		return client{}, err
	}
	if err := validateR3ShadowConfig(cfg); err != nil {
		return client{}, err
	}
	setup, err := connect(cfg, hub, tlsConfig, "live-acceptance-setup")
	if err != nil {
		return client{}, fmt.Errorf("connect to hub for setup: %w", err)
	}
	streamManager, err := modernJetStream(setup.nc, cfg.domain)
	if err != nil {
		setup.nc.Close()
		return client{}, fmt.Errorf("create JetStream manager: %w", err)
	}
	if !cfg.createStream {
		return setup, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), cfg.ackTimeout)
	defer cancel()
	_, err = streamManager.CreateStream(ctx, temporaryStreamConfig(cfg))
	if err != nil {
		setup.nc.Close()
		return client{}, fmt.Errorf("create isolated stream %s: %w", cfg.stream, err)
	}
	return setup, nil
}

func modernJetStream(nc *nats.Conn, domain string) (jsapi.JetStream, error) {
	if domain == "" {
		return jsapi.New(nc)
	}
	return jsapi.NewWithDomain(nc, domain)
}

func temporaryStreamConfig(cfg config) jsapi.StreamConfig {
	stream := jsapi.StreamConfig{
		Name: cfg.stream, Subjects: []string{cfg.subject}, Storage: jsapi.MemoryStorage,
		Replicas: cfg.replicas, MaxBytes: 1 << 30, MaxAge: 5 * time.Minute,
		MaxMsgsPerSubject: 400_000, Retention: jsapi.LimitsPolicy, Discard: jsapi.DiscardOld,
		// Fleet publishers never send Nats-Msg-Id, so this compatibility window
		// remains inert during the benchmark.
		Duplicates:         10 * time.Second,
		AllowAtomicPublish: true, AllowBatchPublish: true,
	}
	if cfg.placementTag != "" {
		stream.Placement = &jsapi.Placement{Tags: []string{cfg.placementTag}}
	}
	return stream
}

func closeSetup(cfg config, setup client) error {
	defer setup.nc.Close()
	if !cfg.cleanup {
		return nil
	}
	if err := validateTemporaryTarget(cfg.stream, cfg.subject); err != nil {
		return fmt.Errorf("refusing unsafe stream cleanup: %w", err)
	}
	if err := setup.js.DeleteStream(cfg.stream); err != nil && !errors.Is(err, nats.ErrStreamNotFound) {
		return fmt.Errorf("cleanup stream %s: %w", cfg.stream, err)
	}
	return nil
}

func clientTLS(caFile string) (*tls.Config, error) {
	if caFile == "" {
		return nil, nil
	}
	pem, err := os.ReadFile(caFile)
	if err != nil {
		return nil, fmt.Errorf("read CA: %w", err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(pem) {
		return nil, errors.New("CA file contains no certificates")
	}
	return &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}, nil
}

func connect(cfg config, ep endpoint, tlsConfig *tls.Config, name string) (client, error) {
	stats := &connectionStats{}
	nc, err := nats.Connect(ep.url, connectionOptions(cfg, tlsConfig, name, stats)...)
	if err != nil {
		return client{}, err
	}
	js, err := legacyJetStream(nc, cfg, ep.domain)
	if err != nil {
		nc.Close()
		return client{}, err
	}
	modern, err := modernJetStream(nc, ep.domain)
	if err != nil {
		nc.Close()
		return client{}, err
	}
	return client{nc: nc, js: js, modern: modern, stats: stats}, nil
}

func connectionOptions(
	cfg config,
	tlsConfig *tls.Config,
	name string,
	stats *connectionStats,
) []nats.Option {
	options := []nats.Option{
		nats.Name(name),
		nats.Timeout(5 * time.Second),
		nats.MaxReconnects(5),
		nats.ReconnectWait(250 * time.Millisecond),
		nats.ReconnectHandler(stats.recordReconnect),
		nats.DisconnectErrHandler(stats.recordDisconnect),
		nats.ErrorHandler(stats.recordAsyncError),
	}
	if cfg.user != "" {
		options = append(options, nats.UserInfo(cfg.user, cfg.password))
	}
	if tlsConfig != nil {
		options = append(options, nats.Secure(tlsConfig.Clone()))
	}
	return options
}

func (s *connectionStats) recordReconnect(*nats.Conn) {
	s.reconnects.Add(1)
}

func (s *connectionStats) recordDisconnect(_ *nats.Conn, err error) {
	if err != nil {
		s.disconnects.Add(1)
	}
}

func (s *connectionStats) recordAsyncError(_ *nats.Conn, _ *nats.Subscription, err error) {
	if err != nil {
		s.asyncErrors.Add(1)
	}
}

func legacyJetStream(nc *nats.Conn, cfg config, domain string) (nats.JetStreamContext, error) {
	options := []nats.JSOpt{
		nats.PublishAsyncMaxPending(cfg.window + 1),
		nats.MaxWait(cfg.ackTimeout),
	}
	if domain != "" {
		options = append(options, nats.Domain(domain))
	}
	return nc.JetStream(options...)
}

func benchmark(cfg config, ep endpoint, tlsConfig *tls.Config) (result, error) {
	mode := cfg.mode + "+dedup-off"
	r := result{
		Endpoint: ep.label, Producer: cfg.producerID, Mode: mode, Messages: cfg.messages,
		Replicas: cfg.replicas, BatchSize: cfg.batchSize, AtomicInflight: cfg.atomicInflight,
		FastOutstanding: cfg.fastOutstanding, PayloadProfile: "varied-eventsub-json",
		LatencyProfile:  "under-load-sync-puback",
		LatencyRequired: latencySampleRequirement(cfg),
	}
	clients, err := benchmarkClients(cfg, ep, tlsConfig)
	if err != nil {
		return r, err
	}
	defer closeClients(clients)
	if err := describeConnection(&r, clients[0], tlsConfig != nil); err != nil {
		return r, err
	}
	payloads := benchmarkPayloads(cfg.payloadBytes, cfg.payloadVariants)
	counters, started, finished, latency, err := runPublishers(cfg, clients, payloads)
	if err != nil {
		return r, err
	}
	measurement := throughputMeasurement{counters: counters, started: started, finished: finished}
	applyThroughput(&r, cfg, measurement)
	applyLatency(&r, latency)
	applyConnectionStats(&r, clients)
	return r, validateBenchmark(cfg, r, counters.firstErr.Load())
}

func benchmarkClients(cfg config, ep endpoint, tlsConfig *tls.Config) ([]client, error) {
	clients := make([]client, 0, cfg.publishers)
	for i := 0; i < cfg.publishers; i++ {
		c, err := connect(cfg, ep, tlsConfig, fmt.Sprintf("live-%s-bench-%d", ep.label, i))
		if err != nil {
			closeClients(clients)
			return nil, fmt.Errorf("connect publisher %d: %w", i, err)
		}
		clients = append(clients, c)
	}
	return clients, nil
}

func describeConnection(r *result, c client, secure bool) error {
	if secure {
		tlsState, err := c.nc.TLSConnectionState()
		if err != nil {
			return fmt.Errorf("connection is not using verified TLS: %w", err)
		}
		r.TLSVersion = tlsVersion(tlsState.Version)
		r.TLSCipher = tls.CipherSuiteName(tlsState.CipherSuite)
	} else {
		r.TLSVersion = "plaintext-local"
	}
	r.Server = c.nc.ConnectedServerName()
	return nil
}

func benchmarkPayloads(size, variants int) [][]byte {
	payloads := make([][]byte, variants)
	for i := range payloads {
		prefix := fmt.Sprintf(
			`{"type":"channel.chat.message","lane":"standard","event_id":"bench-%08x","broadcaster_user_id":"%010d","chatter_user_id":"%010d","text":"`,
			i, i%20_000, (i*7919)%1_000_000,
		)
		suffix := `"}`
		payload := make([]byte, size)
		if len(prefix)+len(suffix) > size {
			fillPayload(payload, uint64(i+1))
		} else {
			copy(payload, prefix)
			fillPayload(payload[len(prefix):size-len(suffix)], uint64(i+1))
			copy(payload[size-len(suffix):], suffix)
		}
		payloads[i] = payload
	}
	return payloads
}

func fillPayload(dst []byte, state uint64) {
	const alphabet = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-_"
	for i := range dst {
		state ^= state << 13
		state ^= state >> 7
		state ^= state << 17
		dst[i] = alphabet[state&63]
	}
}

type benchmarkCounters struct {
	acked    atomic.Int64
	failures atomic.Int64
	timeouts atomic.Int64
	firstErr atomic.Pointer[string]
}

type throughputMeasurement struct {
	counters *benchmarkCounters
	started  time.Time
	finished time.Time
}

func runPublishers(
	cfg config,
	clients []client,
	payloads [][]byte,
) (*benchmarkCounters, time.Time, time.Time, latencyResult, error) {
	counters := &benchmarkCounters{}
	started, err := waitForStart(cfg.startAtUnixMS)
	if err != nil {
		return counters, time.Time{}, time.Time{}, latencyResult{}, err
	}
	loadCtx, cancelLoad := context.WithCancel(context.Background())
	latencyDone := make(chan latencyResult, 1)
	go func() {
		latency := latencyProbe(loadCtx, cfg, clients[0].js, payloads)
		if latency.errors > 0 {
			message := "under-load PubAck latency probe failed"
			counters.firstErr.CompareAndSwap(nil, &message)
			cancelLoad()
		}
		latencyDone <- latency
	}()
	var wg sync.WaitGroup
	for publisher, c := range clients {
		count := cfg.messages / len(clients)
		if publisher < cfg.messages%len(clients) {
			count++
		}
		wg.Add(1)
		go func(publisher int, c client, count int) {
			defer wg.Done()
			job := publishJob{
				ctx: loadCtx, cfg: cfg, client: c, publisher: publisher, count: count,
				payloads: payloads, counters: counters, started: started,
			}
			err := publishByMode(job)
			if err != nil {
				msg := err.Error()
				counters.firstErr.CompareAndSwap(nil, &msg)
				cancelLoad()
			}
		}(publisher, c, count)
	}
	wg.Wait()
	finished := time.Now()
	cancelLoad()
	return counters, started, finished, <-latencyDone, nil
}

func waitForStart(unixMS int64) (time.Time, error) {
	if unixMS <= 0 {
		return time.Now(), nil
	}
	wait := time.Until(time.UnixMilli(unixMS))
	if wait < -time.Second {
		return time.Time{}, fmt.Errorf("missed shared start barrier by %s", -wait)
	}
	if wait > 0 {
		time.Sleep(wait)
	}
	return time.Now(), nil
}

func applyThroughput(
	r *result,
	cfg config,
	measurement throughputMeasurement,
) {
	elapsed := measurement.finished.Sub(measurement.started)
	r.Acknowledged = measurement.counters.acked.Load()
	r.Errors = measurement.counters.failures.Load()
	r.Timeouts = measurement.counters.timeouts.Load()
	r.DurationMS = elapsed.Milliseconds()
	r.StartedUnixMS = measurement.started.UnixMilli()
	r.FinishedUnixMS = measurement.finished.UnixMilli()
	r.MessagesPerSec = float64(r.Acknowledged) / elapsed.Seconds()
	r.MiBPerSec = r.MessagesPerSec * float64(cfg.payloadBytes) / (1024 * 1024)
}

func applyLatency(r *result, latency latencyResult) {
	r.Errors += latency.errors
	r.Timeouts += latency.timeouts
	r.LatencySamples = len(latency.values)
	if len(latency.values) > 0 {
		sort.Slice(latency.values, func(i, j int) bool { return latency.values[i] < latency.values[j] })
		r.PubAckMinMS = durationMS(latency.values[0])
		r.PubAckP50MS = durationMS(percentile(latency.values, 0.50))
		r.PubAckP95MS = durationMS(percentile(latency.values, 0.95))
		r.PubAckP99MS = durationMS(percentile(latency.values, 0.99))
		r.PubAckMaxMS = durationMS(latency.values[len(latency.values)-1])
	}
}

func applyConnectionStats(r *result, clients []client) {
	for _, c := range clients {
		r.Reconnects += c.stats.reconnects.Load()
		r.Disconnects += c.stats.disconnects.Load()
		r.AsyncErrors += c.stats.asyncErrors.Load()
	}
}

func validateBenchmark(cfg config, r result, firstErr *string) error {
	validation := benchmarkValidation{cfg: cfg, result: r, firstErr: firstErr}
	return firstBenchmarkError(
		validation.publishResult,
		validation.latencyCoverage,
		validation.latencyPercentiles,
		validation.connectionStability,
		validation.minimumRate,
	)
}

type benchmarkValidation struct {
	cfg      config
	result   result
	firstErr *string
}

func firstBenchmarkError(checks ...func() error) error {
	for _, check := range checks {
		if err := check(); err != nil {
			return err
		}
	}
	return nil
}

func (v benchmarkValidation) publishResult() error {
	if v.firstErr != nil {
		return errors.New(*v.firstErr)
	}
	if v.result.Errors > 0 {
		return fmt.Errorf(
			"acknowledged %d/%d with %d errors",
			v.result.Acknowledged,
			v.cfg.messages,
			v.result.Errors,
		)
	}
	if v.result.Acknowledged != int64(v.cfg.messages) {
		return fmt.Errorf("acknowledged %d/%d", v.result.Acknowledged, v.cfg.messages)
	}
	return nil
}

func (v benchmarkValidation) latencyCoverage() error {
	if v.cfg.latencySamples > 0 && v.result.LatencySamples == 0 {
		return errors.New("no under-load PubAck latency samples completed")
	}
	if v.result.LatencySamples < v.result.LatencyRequired {
		return fmt.Errorf(
			"completed %d/%d required under-load PubAck latency samples",
			v.result.LatencySamples,
			v.result.LatencyRequired,
		)
	}
	return nil
}

func (v benchmarkValidation) latencyPercentiles() error {
	if time.Duration(v.result.PubAckP95MS*float64(time.Millisecond)) > v.cfg.maxP95 {
		return fmt.Errorf("PubAck p95 %.3fms exceeds %s gate", v.result.PubAckP95MS, v.cfg.maxP95)
	}
	if time.Duration(v.result.PubAckP99MS*float64(time.Millisecond)) > v.cfg.maxP99 {
		return fmt.Errorf("PubAck p99 %.3fms exceeds %s gate", v.result.PubAckP99MS, v.cfg.maxP99)
	}
	return nil
}

func (v benchmarkValidation) connectionStability() error {
	if v.result.Reconnects+v.result.Disconnects+v.result.AsyncErrors+v.result.Timeouts > 0 {
		return fmt.Errorf(
			"connection instability: reconnects=%d disconnects=%d async_errors=%d timeouts=%d",
			v.result.Reconnects,
			v.result.Disconnects,
			v.result.AsyncErrors,
			v.result.Timeouts,
		)
	}
	return nil
}

func (v benchmarkValidation) minimumRate() error {
	if v.cfg.minRate <= 0 {
		return nil
	}
	if v.result.MessagesPerSec < v.cfg.minRate {
		return fmt.Errorf(
			"throughput %.0f/s is below %.0f/s gate",
			v.result.MessagesPerSec,
			v.cfg.minRate,
		)
	}
	return nil
}
