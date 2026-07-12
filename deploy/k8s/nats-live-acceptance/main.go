// Command nats-live-acceptance compares the production JetStream PubAck path
// through the hub and the node-local leaf. It creates an isolated, memory-backed
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
	leafURL        string
	domain         string
	stream         string
	subject        string
	messages       int
	publishers     int
	window         int
	payloadBytes   int
	latencySamples int
	ackTimeout     time.Duration
	maxP95         time.Duration
	mode           string
	endpoints      string
	batchSize      int
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

	hub := endpoint{label: "hub", url: cfg.hubURL}
	leaf := endpoint{label: "leaf", url: cfg.leafURL, domain: cfg.domain}

	setup, err := connect(cfg, hub, tlsConfig, "live-acceptance-setup")
	if err != nil {
		return fmt.Errorf("connect to hub for setup: %w", err)
	}

	var streamManager jsapi.JetStream
	if cfg.domain == "" {
		streamManager, err = jsapi.New(setup.nc)
	} else {
		streamManager, err = jsapi.NewWithDomain(setup.nc, cfg.domain)
	}
	if err != nil {
		setup.nc.Close()
		return fmt.Errorf("create JetStream manager: %w", err)
	}
	if cfg.createStream {
		if _, err := streamManager.CreateStream(context.Background(), jsapi.StreamConfig{
			Name:               cfg.stream,
			Subjects:           []string{cfg.subject},
			Storage:            jsapi.MemoryStorage,
			Replicas:           1,
			MaxBytes:           512 << 20,
			MaxAge:             10 * time.Minute,
			Retention:          jsapi.LimitsPolicy,
			Discard:            jsapi.DiscardOld,
			AllowAtomicPublish: true,
			AllowBatchPublish:  true,
		}); err != nil {
			setup.nc.Close()
			return fmt.Errorf("create isolated stream %s: %w", cfg.stream, err)
		}
	}

	if cfg.cleanup {
		defer func() {
			if err := setup.js.DeleteStream(cfg.stream); err != nil {
				log.Printf("cleanup stream %s failed: %v", cfg.stream, err)
			}
			setup.nc.Close()
		}()
	} else {
		defer setup.nc.Close()
	}
	if cfg.setupOnly {
		fmt.Printf("{\"stream\":%q,\"subject\":%q,\"created\":%t,\"deleted\":%t}\n",
			cfg.stream, cfg.subject, cfg.createStream, cfg.cleanup)
		return nil
	}

	endpoints := selectedEndpoints(cfg, hub, leaf)
	results := make([]result, 0, len(endpoints))
	failed := false
	for _, ep := range endpoints {
		r, err := benchmark(cfg, ep, tlsConfig)
		if err != nil {
			log.Printf("%s benchmark failed: %v", ep.label, err)
			r.Failure = err.Error()
			failed = true
		} else {
			r.Passed = true
		}
		results = append(results, r)
	}

	out, err := json.MarshalIndent(map[string]any{
		"stream":  cfg.stream,
		"subject": cfg.subject,
		"results": results,
	}, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(out))

	if failed {
		return errors.New("one or more NATS acceptance gates failed")
	}
	return nil
}

func parseFlags() config {
	runID := time.Now().UTC().Format("20060102T150405")
	cfg := config{}
	flag.StringVar(&cfg.hubURL, "hub-url", "tls://nats:4222", "direct hub URL")
	flag.StringVar(&cfg.leafURL, "leaf-url", "tls://nats-leaf:4222", "node-local leaf URL")
	flag.StringVar(&cfg.domain, "domain", "hub", "JetStream domain used through the leaf")
	flag.StringVar(&cfg.stream, "stream", "LIVE_NATS_ACCEPTANCE_"+runID, "temporary stream name")
	flag.StringVar(&cfg.subject, "subject", "twitch.outgress.bench."+strings.ToLower(runID), "isolated benchmark subject")
	flag.IntVar(&cfg.messages, "messages", 200_000, "messages published per endpoint")
	flag.IntVar(&cfg.publishers, "publishers", 2, "independent connections per endpoint")
	flag.IntVar(&cfg.window, "window", 16_384, "maximum outstanding PubAcks per publisher")
	flag.IntVar(&cfg.payloadBytes, "payload-bytes", 256, "payload size")
	flag.IntVar(&cfg.latencySamples, "latency-samples", 500, "synchronous PubAck RTT samples per endpoint")
	flag.DurationVar(&cfg.ackTimeout, "ack-timeout", 5*time.Second, "maximum wait for one PubAck")
	flag.DurationVar(&cfg.maxP95, "max-p95", 20*time.Millisecond, "maximum accepted synchronous PubAck p95")
	flag.StringVar(&cfg.mode, "mode", "atomic", "publish mode: async or atomic")
	flag.StringVar(&cfg.endpoints, "endpoints", "both", "endpoints to test: hub, leaf, or both")
	flag.IntVar(&cfg.batchSize, "batch-size", 128, "messages per atomic commit")
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

	if !cfg.insecureLocal && (cfg.user == "" || cfg.password == "" || cfg.caFile == "") {
		log.Fatal("NATS_USER, NATS_PASSWORD and NATS_CA are required")
	}
	if cfg.messages < 1 || cfg.publishers < 1 || cfg.window < 1 || cfg.payloadBytes < 1 || cfg.batchSize < 2 || cfg.batchSize > 1000 {
		log.Fatal("messages, publishers, window and payload-bytes must be positive")
	}
	if cfg.mode != "async" && cfg.mode != "atomic" {
		log.Fatal("mode must be async or atomic")
	}
	if cfg.endpoints != "both" && cfg.endpoints != "hub" && cfg.endpoints != "leaf" {
		log.Fatal("endpoints must be hub, leaf, or both")
	}
	return cfg
}

func hostname() string {
	host, err := os.Hostname()
	if err != nil || host == "" {
		return "unknown"
	}
	return host
}

func selectedEndpoints(cfg config, hub, leaf endpoint) []endpoint {
	switch cfg.endpoints {
	case "hub":
		return []endpoint{hub}
	case "leaf":
		return []endpoint{leaf}
	default:
		return []endpoint{hub, leaf}
	}
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
	r := result{Endpoint: ep.label, Producer: cfg.producerID, Mode: cfg.mode, Messages: cfg.messages}
	clients := make([]client, 0, cfg.publishers)
	for i := 0; i < cfg.publishers; i++ {
		c, err := connect(cfg, ep, tlsConfig, fmt.Sprintf("live-%s-bench-%d", ep.label, i))
		if err != nil {
			closeClients(clients)
			return r, fmt.Errorf("connect publisher %d: %w", i, err)
		}
		clients = append(clients, c)
	}
	defer closeClients(clients)

	if tlsConfig != nil {
		tlsState, err := clients[0].nc.TLSConnectionState()
		if err != nil {
			return r, fmt.Errorf("connection is not using verified TLS: %w", err)
		}
		r.TLSVersion = tlsVersion(tlsState.Version)
		r.TLSCipher = tls.CipherSuiteName(tlsState.CipherSuite)
	} else {
		r.TLSVersion = "plaintext-local"
	}
	r.Server = clients[0].nc.ConnectedServerName()

	payload := make([]byte, cfg.payloadBytes)
	for i := range payload {
		payload[i] = byte('a' + i%26)
	}

	var acked atomic.Int64
	var failures atomic.Int64
	var firstErr atomic.Pointer[string]
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
			var err error
			if cfg.mode == "atomic" {
				err = publishAtomicWindows(cfg, ep, c, publisher, count, payload, &acked, &failures)
			} else {
				err = publishWindows(cfg, ep, c.js, publisher, count, payload, &acked, &failures)
			}
			if err != nil {
				msg := err.Error()
				firstErr.CompareAndSwap(nil, &msg)
			}
		}(publisher, c, count)
	}
	wg.Wait()
	elapsed := time.Since(started)

	r.Acknowledged = acked.Load()
	r.Errors = failures.Load()
	r.DurationMS = elapsed.Milliseconds()
	r.MessagesPerSec = float64(r.Acknowledged) / elapsed.Seconds()
	r.MiBPerSec = r.MessagesPerSec * float64(cfg.payloadBytes) / (1024 * 1024)

	latencies, latencyErrs := latencyProbe(cfg, ep, clients[0].js, payload)
	r.Errors += int64(latencyErrs)
	if len(latencies) > 0 {
		sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
		r.PubAckP50MS = durationMS(percentile(latencies, 0.50))
		r.PubAckP95MS = durationMS(percentile(latencies, 0.95))
		r.PubAckP99MS = durationMS(percentile(latencies, 0.99))
		r.PubAckMaxMS = durationMS(latencies[len(latencies)-1])
	}

	if p := firstErr.Load(); p != nil {
		return r, errors.New(*p)
	}
	if r.Errors > 0 || r.Acknowledged != int64(cfg.messages) {
		return r, fmt.Errorf("acknowledged %d/%d with %d errors", r.Acknowledged, cfg.messages, r.Errors)
	}
	if time.Duration(r.PubAckP95MS*float64(time.Millisecond)) > cfg.maxP95 {
		return r, fmt.Errorf("PubAck p95 %.3fms exceeds %s gate", r.PubAckP95MS, cfg.maxP95)
	}
	if cfg.minRate > 0 && r.MessagesPerSec < cfg.minRate {
		return r, fmt.Errorf("throughput %.0f/s is below %.0f/s gate", r.MessagesPerSec, cfg.minRate)
	}
	return r, nil
}

func publishAtomicWindows(
	cfg config,
	ep endpoint,
	c client,
	publisher int,
	count int,
	payload []byte,
	acked *atomic.Int64,
	failures *atomic.Int64,
) error {
	inbox := nats.NewInbox()
	replies, err := c.nc.SubscribeSync(inbox + ".>")
	if err != nil {
		return err
	}
	defer replies.Unsubscribe() //nolint:errcheck -- connection close is authoritative

	for offset := 0; offset < count; offset += cfg.batchSize {
		size := min(cfg.batchSize, count-offset)
		batchID := fmt.Sprintf("%s-%d-%d-%d", cfg.producerID, publisher, offset, time.Now().UnixNano())
		reply := inbox + "." + batchID

		for i := 0; i < size; i++ {
			sequence := offset + i
			msg := nats.NewMsg(cfg.subject)
			msg.Data = payload
			msg.Header.Set(nats.MsgIdHdr, fmt.Sprintf("live-%s-%d-%d", cfg.producerID, publisher, sequence))
			msg.Header.Set("Nats-Batch-Id", batchID)
			msg.Header.Set("Nats-Batch-Sequence", fmt.Sprint(i+1))
			msg.Header.Set("Nats-Required-Api-Level", "2")
			if i == size-1 {
				msg.Header.Set("Nats-Batch-Commit", "1")
				msg.Reply = reply
			}
			if err := c.nc.PublishMsg(msg); err != nil {
				failures.Add(int64(size))
				return fmt.Errorf("publisher %d atomic batch %d member %d: %w", publisher, offset/cfg.batchSize, i, err)
			}
		}

		ackMsg, err := replies.NextMsg(cfg.ackTimeout)
		if err != nil {
			failures.Add(int64(size))
			return fmt.Errorf("publisher %d atomic batch %d ack: %w", publisher, offset/cfg.batchSize, err)
		}
		var ack jetStreamBatchAck
		if err := json.Unmarshal(ackMsg.Data, &ack); err != nil {
			failures.Add(int64(size))
			return err
		}
		if ack.Error != nil || ack.Batch != batchID || ack.Count != size || ack.Sequence == 0 {
			failures.Add(int64(size))
			return fmt.Errorf("publisher %d invalid batch ack: batch=%q count=%d seq=%d error=%v",
				publisher, ack.Batch, ack.Count, ack.Sequence, ack.Error)
		}
		acked.Add(int64(size))
	}
	return nil
}

type jetStreamBatchAck struct {
	Sequence uint64          `json:"seq"`
	Batch    string          `json:"batch"`
	Count    int             `json:"count"`
	Error    json.RawMessage `json:"error,omitempty"`
}

func publishWindows(
	cfg config,
	ep endpoint,
	js nats.JetStreamContext,
	publisher int,
	count int,
	payload []byte,
	acked *atomic.Int64,
	failures *atomic.Int64,
) error {
	type pending struct {
		future nats.PubAckFuture
	}

	for offset := 0; offset < count; offset += cfg.window {
		size := min(cfg.window, count-offset)
		batch := make([]pending, 0, size)
		for i := 0; i < size; i++ {
			sequence := offset + i
			msg := nats.NewMsg(cfg.subject)
			msg.Data = payload
			msg.Header.Set(nats.MsgIdHdr, fmt.Sprintf("live-%s-%d-%d", cfg.producerID, publisher, sequence))
			future, err := js.PublishMsgAsync(msg)
			if err != nil {
				failures.Add(1)
				return fmt.Errorf("publisher %d sequence %d enqueue: %w", publisher, sequence, err)
			}
			batch = append(batch, pending{future: future})
		}

		deadline := time.NewTimer(cfg.ackTimeout)
		for i, item := range batch {
			select {
			case <-item.future.Ok():
				acked.Add(1)
			case err := <-item.future.Err():
				failures.Add(1)
				deadline.Stop()
				return fmt.Errorf("publisher %d PubAck %d: %w", publisher, offset+i, err)
			case <-deadline.C:
				failures.Add(int64(len(batch) - i))
				return fmt.Errorf("publisher %d timed out with %d PubAcks pending", publisher, len(batch)-i)
			}
		}
		if !deadline.Stop() {
			select {
			case <-deadline.C:
			default:
			}
		}
	}
	return nil
}

func latencyProbe(cfg config, ep endpoint, js nats.JetStreamContext, payload []byte) ([]time.Duration, int) {
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
