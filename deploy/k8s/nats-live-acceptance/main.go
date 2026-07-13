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
	"flag"
	"fmt"
	"log"
	"math"
	"os"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/nats-io/nats.go"
	jsapi "github.com/nats-io/nats.go/jetstream"
)

type config struct {
	hubURL         string
	domain         string
	placementTag   string
	stream         string
	subject        string
	messages       int
	publishers     int
	window         int
	payloadBytes   int
	latencySamples int
	ackTimeout     time.Duration
	maxP95         time.Duration
	minRate        float64
	cleanup        bool
	createStream   bool
	setupOnly      bool
	producerID     string
	insecureLocal  bool
	user           string
	password       string
	caFile         string
}

type endpoint struct {
	label  string
	url    string
	domain string
}

type result struct {
	Endpoint       string  `json:"endpoint"`
	Producer       string  `json:"producer"`
	Mode           string  `json:"mode"`
	Server         string  `json:"server"`
	TLSVersion     string  `json:"tls_version"`
	TLSCipher      string  `json:"tls_cipher"`
	Messages       int     `json:"messages"`
	Acknowledged   int64   `json:"acknowledged"`
	Errors         int64   `json:"errors"`
	DurationMS     int64   `json:"duration_ms"`
	MessagesPerSec float64 `json:"messages_per_second"`
	MiBPerSec      float64 `json:"mib_per_second"`
	PubAckP50MS    float64 `json:"puback_p50_ms"`
	PubAckP95MS    float64 `json:"puback_p95_ms"`
	PubAckP99MS    float64 `json:"puback_p99_ms"`
	PubAckMaxMS    float64 `json:"puback_max_ms"`
	Passed         bool    `json:"passed"`
	Failure        string  `json:"failure,omitempty"`
}

type client struct {
	nc *nats.Conn
	js nats.JetStreamContext
}

func main() {
	if err := run(); err != nil {
		log.Print(err)
		os.Exit(1)
	}
}

func run() error {
	cfg := parseFlags()
	tlsConfig, err := clientTLS(cfg.caFile)
	if err != nil {
		return err
	}
	hub := endpoint{label: "hub", url: cfg.hubURL, domain: cfg.domain}
	setup, err := prepareStream(cfg, hub, tlsConfig)
	if err != nil {
		return err
	}
	defer closeSetup(cfg, setup)

	if cfg.setupOnly {
		fmt.Printf("{\"stream\":%q,\"subject\":%q,\"created\":%t,\"deleted\":%t}\n",
			cfg.stream, cfg.subject, cfg.createStream, cfg.cleanup)
		return nil
	}

	r, benchmarkErr := benchmark(cfg, hub, tlsConfig)
	if benchmarkErr != nil {
		log.Printf("hub benchmark failed: %v", benchmarkErr)
		r.Failure = benchmarkErr.Error()
	} else {
		r.Passed = true
	}
	out, err := json.MarshalIndent(map[string]any{
		"stream":  cfg.stream,
		"subject": cfg.subject,
		"results": []result{r},
	}, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(out))

	if benchmarkErr != nil {
		return errors.New("one or more NATS acceptance gates failed")
	}
	return nil
}

func prepareStream(cfg config, hub endpoint, tlsConfig *tls.Config) (client, error) {
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
	_, err = streamManager.CreateStream(context.Background(), temporaryStreamConfig(cfg))
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
		Replicas: 1, MaxBytes: 512 << 20, MaxAge: 10 * time.Minute,
		Retention: jsapi.LimitsPolicy, Discard: jsapi.DiscardOld,
		// Match TWITCH_INGRESS. The server default is two minutes, which keeps a
		// huge synthetic-ID index and benchmarks dedup memory rather than the
		// production stream's ten-second retry horizon.
		Duplicates:         10 * time.Second,
		AllowAtomicPublish: true, AllowBatchPublish: true,
	}
	if cfg.placementTag != "" {
		stream.Placement = &jsapi.Placement{Tags: []string{cfg.placementTag}}
	}
	return stream
}

func closeSetup(cfg config, setup client) {
	defer setup.nc.Close()
	if !cfg.cleanup {
		return
	}
	if err := setup.js.DeleteStream(cfg.stream); err != nil {
		log.Printf("cleanup stream %s failed: %v", cfg.stream, err)
	}
}

func parseFlags() config {
	runID := time.Now().UTC().Format("20060102T150405")
	cfg := config{}
	flag.StringVar(&cfg.hubURL, "hub-url", "tls://nats:4222", "direct hub URL")
	flag.StringVar(&cfg.domain, "domain", "hub", "hub JetStream domain")
	flag.StringVar(&cfg.placementTag, "placement-tag", "", "required server tag for the temporary R1 stream")
	flag.StringVar(&cfg.stream, "stream", "LIVE_NATS_ACCEPTANCE_"+runID, "temporary stream name")
	flag.StringVar(&cfg.subject, "subject", "twitch.outgress.bench."+strings.ToLower(runID), "isolated benchmark subject")
	flag.IntVar(&cfg.messages, "messages", 200_000, "messages published per endpoint")
	flag.IntVar(&cfg.publishers, "publishers", 2, "independent connections per endpoint")
	flag.IntVar(&cfg.window, "window", 16_384, "maximum outstanding PubAcks per publisher")
	flag.IntVar(&cfg.payloadBytes, "payload-bytes", 256, "payload size")
	flag.IntVar(&cfg.latencySamples, "latency-samples", 500, "synchronous PubAck RTT samples per endpoint")
	flag.DurationVar(&cfg.ackTimeout, "ack-timeout", 5*time.Second, "maximum wait for one PubAck")
	flag.DurationVar(&cfg.maxP95, "max-p95", 20*time.Millisecond, "maximum accepted synchronous PubAck p95")
	flag.Float64Var(&cfg.minRate, "min-rate", 0, "minimum accepted messages/second per endpoint (0 disables)")
	flag.BoolVar(&cfg.cleanup, "cleanup", true, "delete the temporary stream on exit")
	flag.BoolVar(&cfg.createStream, "create-stream", true, "create the isolated stream before benchmarking")
	flag.BoolVar(&cfg.setupOnly, "setup-only", false, "perform create/cleanup actions without benchmarking")
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
	requirePositive(cfg.payloadBytes, "payload-bytes")
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
	connOpts := []nats.Option{
		nats.Name(name),
		nats.Timeout(5 * time.Second),
		nats.MaxReconnects(5),
		nats.ReconnectWait(250 * time.Millisecond),
	}
	if cfg.user != "" {
		connOpts = append(connOpts, nats.UserInfo(cfg.user, cfg.password))
	}
	if tlsConfig != nil {
		connOpts = append(connOpts, nats.Secure(tlsConfig.Clone()))
	}
	nc, err := nats.Connect(ep.url, connOpts...)
	if err != nil {
		return client{}, err
	}

	opts := []nats.JSOpt{
		nats.PublishAsyncMaxPending(cfg.window + 1),
		nats.MaxWait(cfg.ackTimeout),
	}
	if ep.domain != "" {
		opts = append(opts, nats.Domain(ep.domain))
	}
	js, err := nc.JetStream(opts...)
	if err != nil {
		nc.Close()
		return client{}, err
	}
	return client{nc: nc, js: js}, nil
}

func benchmark(cfg config, ep endpoint, tlsConfig *tls.Config) (result, error) {
	r := result{Endpoint: ep.label, Producer: cfg.producerID, Mode: "nats.go-async-puback", Messages: cfg.messages}
	clients, err := benchmarkClients(cfg, ep, tlsConfig)
	if err != nil {
		return r, err
	}
	defer closeClients(clients)
	if err := describeConnection(&r, clients[0], tlsConfig != nil); err != nil {
		return r, err
	}
	payload := benchmarkPayload(cfg.payloadBytes)
	counters, elapsed := runPublishers(cfg, clients, payload)
	applyThroughput(&r, cfg, counters, elapsed)
	applyLatency(&r, cfg, clients[0].js, payload)
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

func benchmarkPayload(size int) []byte {
	payload := make([]byte, size)
	for i := range payload {
		payload[i] = byte('a' + i%26)
	}
	return payload
}

type benchmarkCounters struct {
	acked    atomic.Int64
	failures atomic.Int64
	firstErr atomic.Pointer[string]
}

func runPublishers(cfg config, clients []client, payload []byte) (*benchmarkCounters, time.Duration) {
	counters := &benchmarkCounters{}
	started := time.Now()
	var wg sync.WaitGroup
	for publisher, c := range clients {
		count := cfg.messages / len(clients)
		if publisher < cfg.messages%len(clients) {
			count++
		}
		wg.Add(1)
		go func(publisher int, c client, count int) {
			defer wg.Done()
			job := publishJob{cfg: cfg, client: c, publisher: publisher, count: count, payload: payload, counters: counters}
			err := publishWindows(job)
			if err != nil {
				msg := err.Error()
				counters.firstErr.CompareAndSwap(nil, &msg)
			}
		}(publisher, c, count)
	}
	wg.Wait()
	return counters, time.Since(started)
}

func applyThroughput(r *result, cfg config, counters *benchmarkCounters, elapsed time.Duration) {
	r.Acknowledged = counters.acked.Load()
	r.Errors = counters.failures.Load()
	r.DurationMS = elapsed.Milliseconds()
	r.MessagesPerSec = float64(r.Acknowledged) / elapsed.Seconds()
	r.MiBPerSec = r.MessagesPerSec * float64(cfg.payloadBytes) / (1024 * 1024)
}

func applyLatency(r *result, cfg config, js nats.JetStreamContext, payload []byte) {
	latencies, latencyErrs := latencyProbe(cfg, js, payload)
	r.Errors += int64(latencyErrs)
	if len(latencies) > 0 {
		sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
		r.PubAckP50MS = durationMS(percentile(latencies, 0.50))
		r.PubAckP95MS = durationMS(percentile(latencies, 0.95))
		r.PubAckP99MS = durationMS(percentile(latencies, 0.99))
		r.PubAckMaxMS = durationMS(latencies[len(latencies)-1])
	}
}

func validateBenchmark(cfg config, r result, firstErr *string) error {
	if firstErr != nil {
		return errors.New(*firstErr)
	}
	if r.Errors > 0 {
		return fmt.Errorf("acknowledged %d/%d with %d errors", r.Acknowledged, cfg.messages, r.Errors)
	}
	if r.Acknowledged != int64(cfg.messages) {
		return fmt.Errorf("acknowledged %d/%d", r.Acknowledged, cfg.messages)
	}
	if time.Duration(r.PubAckP95MS*float64(time.Millisecond)) > cfg.maxP95 {
		return fmt.Errorf("PubAck p95 %.3fms exceeds %s gate", r.PubAckP95MS, cfg.maxP95)
	}
	if cfg.minRate <= 0 {
		return nil
	}
	if r.MessagesPerSec < cfg.minRate {
		return fmt.Errorf("throughput %.0f/s is below %.0f/s gate", r.MessagesPerSec, cfg.minRate)
	}
	return nil
}

type publishJob struct {
	cfg       config
	client    client
	publisher int
	count     int
	payload   []byte
	counters  *benchmarkCounters
}

func publishWindows(job publishJob) error {
	for offset := 0; offset < job.count; offset += job.cfg.window {
		size := min(job.cfg.window, job.count-offset)
		batch, err := enqueueWindow(job, offset, size)
		if err != nil {
			return err
		}
		if err := awaitWindow(job, offset, batch); err != nil {
			return err
		}
	}
	return nil
}

type pendingPublish struct {
	future nats.PubAckFuture
}

func enqueueWindow(job publishJob, offset, size int) ([]pendingPublish, error) {
	batch := make([]pendingPublish, 0, size)
	for i := 0; i < size; i++ {
		sequence := offset + i
		msg := nats.NewMsg(job.cfg.subject)
		msg.Data = job.payload
		msg.Header.Set(nats.MsgIdHdr, fmt.Sprintf("live-%s-%d-%d", job.cfg.producerID, job.publisher, sequence))
		future, err := job.client.js.PublishMsgAsync(msg)
		if err != nil {
			job.counters.failures.Add(1)
			return nil, fmt.Errorf("publisher %d sequence %d enqueue: %w", job.publisher, sequence, err)
		}
		batch = append(batch, pendingPublish{future: future})
	}
	return batch, nil
}

func awaitWindow(job publishJob, offset int, batch []pendingPublish) error {
	deadline := time.NewTimer(job.cfg.ackTimeout)
	defer stopAndDrain(deadline)
	for i, item := range batch {
		if err := awaitFuture(item.future, deadline.C); err != nil {
			job.counters.failures.Add(int64(len(batch) - i))
			return fmt.Errorf("publisher %d PubAck %d: %w", job.publisher, offset+i, err)
		}
		job.counters.acked.Add(1)
	}
	return nil
}

func awaitFuture(future nats.PubAckFuture, timeout <-chan time.Time) error {
	select {
	case <-future.Ok():
		return nil
	case err := <-future.Err():
		return err
	case <-timeout:
		return errors.New("timed out")
	}
}

func stopAndDrain(timer *time.Timer) {
	if timer.Stop() {
		return
	}
	select {
	case <-timer.C:
	default:
	}
}

func latencyProbe(cfg config, js nats.JetStreamContext, payload []byte) ([]time.Duration, int) {
	latencies := make([]time.Duration, 0, cfg.latencySamples)
	errors := 0
	for i := 0; i < cfg.latencySamples; i++ {
		msg := nats.NewMsg(cfg.subject)
		msg.Data = payload
		msg.Header.Set(nats.MsgIdHdr, fmt.Sprintf("latency-%s-%d", cfg.producerID, i))
		started := time.Now()
		if _, err := js.PublishMsg(msg); err != nil {
			errors++
			continue
		}
		latencies = append(latencies, time.Since(started))
	}
	return latencies, errors
}

func percentile(values []time.Duration, p float64) time.Duration {
	index := int(math.Ceil(float64(len(values))*p)) - 1
	if index < 0 {
		index = 0
	}
	return values[index]
}

func durationMS(value time.Duration) float64 {
	return float64(value.Microseconds()) / 1000
}

func tlsVersion(version uint16) string {
	switch version {
	case tls.VersionTLS13:
		return "TLS 1.3"
	case tls.VersionTLS12:
		return "TLS 1.2"
	default:
		return fmt.Sprintf("0x%x", version)
	}
}

func closeClients(clients []client) {
	for _, c := range clients {
		c.nc.Close()
	}
}
