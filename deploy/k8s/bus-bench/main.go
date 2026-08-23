// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/binary"
	"errors"
	"flag"
	"fmt"
	"math"
	"os"
	"slices"
	"strings"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/nats-io/nats.go"
	jsapi "github.com/nats-io/nats.go/jetstream"
	"go.uber.org/zap"

	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/codec"
)

const (
	fallbackURL       = "nats://nats.messaging.svc.cluster.local:4222"
	maxLatencySamples = 12_000_000
	dupMapLimit       = 1_000_000
)

type latencyStats struct {
	Count int64   `json:"count"`
	Min   int64   `json:"min"`
	Avg   float64 `json:"avg"`
	Max   int64   `json:"max"`
	P50   int64   `json:"p50"`
	P99   int64   `json:"p99"`
}

type publishReport struct {
	Admitted    uint64       `json:"admitted"`
	Errors      uint64       `json:"errors"`
	ElapsedS    float64      `json:"elapsed_s"`
	OfferedRate float64      `json:"offered_rate"`
	CommitNs    latencyStats `json:"commit_latency_ns"`
}

type consumeReport struct {
	Consumed   uint64       `json:"consumed"`
	Rate       float64      `json:"rate"`
	E2ENs      latencyStats `json:"e2e_latency_ns"`
	Duplicates uint64       `json:"duplicates"`
}

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

type benchLane struct {
	url     string
	stream  string
	subject string
	group   string
}

// unixNano is a wall-clock instant in nanoseconds since the epoch, the form
// the bench's payloads and measurement windows carry.
type unixNano int64

// wait blocks until the instant arrives; a zero or past instant is immediate.
func (t unixNano) wait() {
	if t <= 0 {
		return
	}
	if d := time.Until(time.Unix(0, int64(t))); d > 0 {
		time.Sleep(d)
	}
}

func emit(report any) {
	b, merr := codec.Marshal(report)
	if merr != nil {
		fmt.Fprintln(os.Stderr, "bus-bench: marshal report:", merr)
		os.Exit(1)
	}
	fmt.Printf("RIG_REPORT: %s\n", b)
}

func loadCA() (*x509.CertPool, error) {
	caPEM := os.Getenv("NATS_CA_PEM")
	if caPEM == "" {
		return nil, nil
	}
	data := []byte(caPEM)
	if strings.HasPrefix(caPEM, "/") {
		b, rerr := os.ReadFile(caPEM)
		if rerr != nil {
			return nil, fmt.Errorf("read NATS_CA_PEM: %w", rerr)
		}
		data = b
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(data) {
		return nil, errors.New("NATS_CA_PEM contains no parseable PEM certificates")
	}
	return pool, nil
}

func baseConnectOptions() []nats.Option {
	return []nats.Option{
		nats.UserInfo(os.Getenv("NATS_USER"), os.Getenv("NATS_PASSWORD")),
		nats.Timeout(15 * time.Second),
	}
}

// clientTLSConfig wraps a CA pool with the client key pair the hub's
// verify:true listeners ask for. pkg/bus presents this pair on its own
// connections (connect.go), and the management connection must answer the same
// certificate request or the dial is refused.
func clientTLSConfig(pool *x509.CertPool) *tls.Config {
	cfg := &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}
	certFile, keyFile := os.Getenv("NATS_CLIENT_CERT_FILE"), os.Getenv("NATS_CLIENT_KEY_FILE")
	if certFile == "" || keyFile == "" {
		return cfg
	}
	cfg.GetClientCertificate = func(*tls.CertificateRequestInfo) (*tls.Certificate, error) {
		cert, cerr := tls.LoadX509KeyPair(certFile, keyFile)
		if cerr != nil {
			return nil, fmt.Errorf("load nats client key pair: %w", cerr)
		}
		return &cert, nil
	}
	return cfg
}

// jetStreamFor opens the management JetStream view on nc. jsapi.NewWithDomain
// refuses an empty domain; the fleet's JetStream plane lives in the "hub"
// domain (pkg/bus JSDomain default), which a direct-to-hub dial must name.
func jetStreamFor(nc *nats.Conn) (jsapi.JetStream, error) {
	domain := os.Getenv("NATS_JS_DOMAIN")
	if domain == "" {
		domain = "hub"
	}
	return jsapi.NewWithDomain(nc, domain)
}

func mgmtConnect(url string) (*nats.Conn, jsapi.JetStream, error) {
	opts := baseConnectOptions()
	pool, err := loadCA()
	if err != nil {
		return nil, nil, err
	}
	if pool != nil {
		opts = append(opts, nats.Secure(clientTLSConfig(pool)))
	}
	nc, err := nats.Connect(url, opts...)
	if err != nil {
		return nil, nil, err
	}
	js, err := jetStreamFor(nc)
	if err != nil {
		nc.Close()
		return nil, nil, err
	}
	return nc, js, nil
}

func benchStreamConfig(name string, maxBytes int64) jsapi.StreamConfig {
	spec := bus.TwitchIngressRetryStream
	return jsapi.StreamConfig{
		Name:               name,
		Subjects:           append([]string(nil), spec.Subjects...),
		Retention:          jsapi.LimitsPolicy,
		Storage:            jsapi.MemoryStorage,
		Replicas:           spec.Replicas,
		MaxAge:             spec.MaxAge,
		MaxBytes:           maxBytes,
		Duplicates:         10 * time.Second,
		AllowMsgSchedules:  spec.MsgSchedules,
		AllowMsgTTL:        spec.MsgSchedules,
		AllowAtomicPublish: spec.BatchPublish,
		AllowBatchPublish:  spec.BatchPublish,
	}
}

func runSetup(lane benchLane, maxBytes int64) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	nc, js, err := mgmtConnect(lane.url)
	if err != nil {
		return err
	}
	defer nc.Close()

	st, err := js.Stream(ctx, lane.stream)
	switch {
	case errors.Is(err, jsapi.ErrStreamNotFound):
		if _, cerr := js.CreateStream(ctx, benchStreamConfig(lane.stream, maxBytes)); cerr != nil {
			return cerr
		}
		emit(map[string]any{"created": true})
	case err != nil:
		return err
	default:
		cfg := st.CachedInfo().Config
		if cfg.MaxBytes < maxBytes {
			original := cfg.MaxBytes
			cfg.MaxBytes = maxBytes
			if _, uerr := js.UpdateStream(ctx, cfg); uerr != nil {
				return uerr
			}
			emit(map[string]any{"created": false, "original_max_bytes": original})
			return nil
		}
		emit(map[string]any{"created": false, "raised": false})
	}
	return nil
}

func durableFor(lane benchLane) string {
	return lane.group + "_" + strings.NewReplacer(".", "_", "*", "_", ">", "_").Replace(lane.subject)
}

// deleteBenchConsumer removes the bench durable, tolerating its absence.
func deleteBenchConsumer(ctx context.Context, js jsapi.JetStream, streamName, durable string) (bool, error) {
	err := js.DeleteConsumer(ctx, streamName, durable)
	if errors.Is(err, jsapi.ErrConsumerNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// revertStreamMaxBytes restores the bench stream's original MaxBytes cap,
// tolerating a stream that setup never created.
func revertStreamMaxBytes(ctx context.Context, js jsapi.JetStream, streamName string, original int64) (bool, error) {
	if original <= 0 {
		return false, nil
	}
	st, serr := js.Stream(ctx, streamName)
	if errors.Is(serr, jsapi.ErrStreamNotFound) {
		return false, nil
	}
	if serr != nil {
		return false, serr
	}
	cfg := st.CachedInfo().Config
	if cfg.MaxBytes == original {
		return false, nil
	}
	cfg.MaxBytes = original
	if _, uerr := js.UpdateStream(ctx, cfg); uerr != nil {
		return false, uerr
	}
	return true, nil
}

func runCleanup(lane benchLane, originalMaxBytes int64) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	nc, js, err := mgmtConnect(lane.url)
	if err != nil {
		return err
	}
	defer nc.Close()

	durable := durableFor(lane)

	report := map[string]any{"deleted_consumer": false, "reverted_max_bytes": false}
	deleted, err := deleteBenchConsumer(ctx, js, lane.stream, durable)
	if err != nil {
		return err
	}
	report["deleted_consumer"] = deleted
	report["consumer"] = durable

	reverted, err := revertStreamMaxBytes(ctx, js, lane.stream, originalMaxBytes)
	if err != nil {
		return err
	}
	report["reverted_max_bytes"] = reverted
	if reverted {
		report["max_bytes"] = originalMaxBytes
	}

	emit(report)
	return nil
}

func buildPayload(seq uint64, sent unixNano, size int) []byte {
	if size < 16 {
		size = 16
	}
	buf := make([]byte, size)
	binary.BigEndian.PutUint64(buf[0:8], seq)
	binary.BigEndian.PutUint64(buf[8:16], uint64(sent))
	for i := 16; i < size; i++ {
		buf[i] = byte(seq>>uint(i%7)) ^ byte(i)
	}
	return buf
}

func summarize(sorted []int64) latencyStats {
	if len(sorted) == 0 {
		return latencyStats{}
	}
	var sum int64
	for _, v := range sorted {
		sum += v
	}
	n := int64(len(sorted))
	return latencyStats{
		Count: n,
		Min:   sorted[0],
		Avg:   float64(sum) / float64(n),
		Max:   sorted[n-1],
		P50:   nearestRank(sorted, 50),
		P99:   nearestRank(sorted, 99),
	}
}

func nearestRank(sorted []int64, p float64) int64 {
	n := len(sorted)
	rank := int(math.Ceil(p / 100 * float64(n)))
	if rank < 1 {
		rank = 1
	}
	if rank > n {
		rank = n
	}
	return sorted[rank-1]
}

func sortAsc(v []int64) {
	slices.Sort(v)
}

// publishOpts bundles one publish-mode invocation. Every flag feeds the same
// run, so they travel as a struct rather than as a nine-argument signature.
type publishOpts struct {
	lane         benchLane
	duration     time.Duration
	startAt      unixNano
	payloadSize  int
	confirmEvery int
	rate         int
	podIndex     int
	feeders      int
}

func runPublish(o publishOpts) error {
	o.startAt.wait()
	windowStart := time.Now()
	deadline := windowStart.Add(o.duration)

	pub, err := bus.NewPublisher(o.lane.url, zap.NewNop())
	if err != nil {
		return err
	}
	defer pub.Close()

	ctx := context.Background()
	var admitted, pErrs uint64
	samples := collectFeedSamples(ctx, pub, o, deadline, &admitted, &pErrs)

	flushCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	_ = pub.Flush(flushCtx)
	cancel()

	elapsed := time.Since(windowStart)
	sortAsc(samples)
	emit(publishReport{
		Admitted:    admitted,
		Errors:      pErrs,
		ElapsedS:    elapsed.Seconds(),
		OfferedRate: float64(admitted) / elapsed.Seconds(),
		CommitNs:    summarize(samples),
	})
	return nil
}

// collectFeedSamples drives every feeder goroutine to its deadline and returns
// their merged commit-latency samples.
func collectFeedSamples(ctx context.Context, pub bus.Publisher, o publishOpts, deadline time.Time, admitted, pErrs *uint64) []int64 {
	feeders := max(o.feeders, 1)
	samplesCh := make(chan []int64, feeders)
	var wg sync.WaitGroup
	for f := range feeders {
		wg.Add(1)
		go func() {
			defer wg.Done()
			samplesCh <- runFeeder(bus.WithPublishPartition(ctx, strconv.Itoa(f)), pub, o, f, deadline, admitted, pErrs)
		}()
	}
	wg.Wait()
	close(samplesCh)
	var samples []int64
	for s := range samplesCh {
		samples = append(samples, s...)
	}
	return samples
}

// feedPacer spaces a feeder's publishes so the pool offers rate/feeders msg/s
// in aggregate; disabled when no rate was requested.
type feedPacer struct {
	on    bool
	slot  time.Time
	stride time.Duration
}

func newFeedPacer(rate, feeders int) feedPacer {
	if rate <= 0 {
		return feedPacer{}
	}
	return feedPacer{on: true, slot: time.Now(), stride: time.Second * time.Duration(feeders) / time.Duration(rate)}
}

func (p *feedPacer) wait() {
	if !p.on {
		return
	}
	p.slot = p.slot.Add(p.stride)
	if d := time.Until(p.slot); d > 0 {
		time.Sleep(d)
	}
}

// publishOne sends one message, confirmed (commit latency sampled) or raw.
func publishOne(ctx context.Context, pub bus.Publisher, subject, id string, body []byte, confirmed bool) (time.Duration, error) {
	t0 := time.Now()
	var err error
	if confirmed {
		err = bus.PublishConfirmed(ctx, pub, bus.Publication{Subject: subject, ID: id, Payload: body})
	} else {
		err = bus.PublishRaw(ctx, pub, subject, body)
	}
	return time.Since(t0), err
}

// runFeeder publishes under one partition until deadline. hashStreamRouter pins
// one routing key to ONE pooled connection, so an unpartitioned feeder engages
// exactly one worker of the pool and the other members never build a worker at
// all. One feeder per pooled connection, each under its own publish partition,
// is the minimum shape that exercises the whole publisher; ordering is
// per-feeder, which is all the latency samples need.
func runFeeder(ctx context.Context, pub bus.Publisher, o publishOpts, f int, deadline time.Time, admitted, pErrs *uint64) []int64 {
	pacer := newFeedPacer(o.rate, max(o.feeders, 1))
	seq := uint64(f)
	var samples []int64
	for time.Now().Before(deadline) {
		pacer.wait()
		seq += uint64(max(o.feeders, 1))
		globalSeq := uint64(o.podIndex)<<48 | seq
		body := buildPayload(globalSeq, unixNano(time.Now().UnixNano()), o.payloadSize)
		confirmed := o.confirmEvery > 0 && seq%uint64(o.confirmEvery) == 0
		id := fmt.Sprintf("bench-%d-%d", o.podIndex, seq)
		elapsed, err := publishOne(ctx, pub, o.lane.subject, id, body, confirmed)
		if err != nil {
			atomic.AddUint64(pErrs, 1)
		} else {
			atomic.AddUint64(admitted, 1)
		}
		if confirmed {
			samples = append(samples, elapsed.Nanoseconds())
		}
	}
	return samples
}

type collector struct {
	winStart unixNano
	winEnd   unixNano

	lat      []int64
	latIdx   atomic.Int64
	consumed atomic.Int64

	mu       sync.Mutex
	seen     map[uint64]struct{}
	dupes    uint64
	tracking bool
}

// measuring reports whether deliveries arriving at now count toward this
// run's measurement window.
func (c *collector) measuring(now unixNano) bool {
	return now >= c.winStart && now < c.winEnd
}

func (c *collector) handle(msg *bus.Message) error {
	now := unixNano(time.Now().UnixNano())
	defer msg.Ack()

	p := msg.Payload
	if len(p) < 16 || !c.measuring(now) {
		return nil
	}
	seq := binary.BigEndian.Uint64(p[0:8])
	sentNs := unixNano(binary.BigEndian.Uint64(p[8:16]))
	if i := c.latIdx.Add(1); i <= int64(len(c.lat)) {
		c.lat[i-1] = int64(now - sentNs)
	}
	c.consumed.Add(1)
	c.noteSeq(seq)
	return nil
}

func (c *collector) noteSeq(seq uint64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.tracking || len(c.seen) >= dupMapLimit {
		return
	}
	if _, dup := c.seen[seq]; dup {
		c.dupes++
		return
	}
	c.seen[seq] = struct{}{}
}

func runConsume(lane benchLane, duration time.Duration, startAt unixNano, payloadSize int) error {
	if startAt == 0 {
		startAt = unixNano(time.Now().UnixNano())
	}
	winStart, winEnd := startAt, startAt+unixNano(duration.Nanoseconds())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sub, err := bus.NewSubscriber(lane.url, lane.group, zap.NewNop())
	if err != nil {
		return err
	}

	c := &collector{
		winStart: winStart,
		winEnd:   winEnd,
		lat:      make([]int64, maxLatencySamples),
		seen:     make(map[uint64]struct{}, 1024),
		tracking: true,
	}
	// One consumption path only: ConsumeWeighted owns the lane binding. A
	// second raw drain here would subscribe the same durable a second time
	// (its own connection and queue membership), splitting deliveries down a
	// path whose acknowledgements race the weighted pool's.
	w, err := bus.ConsumeWeighted(ctx, nil, []bus.WeightedLane{{
		Sub:     sub,
		Subject: lane.subject,
		Handle:  c.handle,
	}}, bus.ScalePolicy{MinRoutines: 256, MaxRoutines: 512}, zap.NewNop())
	if err != nil {
		return err
	}

	winStart.wait()
	winEnd.wait()

	cancel()
	drainCtx, drainCancel := context.WithTimeout(context.Background(), 5*time.Second)
	_ = w.Drain(drainCtx)
	drainCancel()
	_ = sub.Close()

	measured := c.lat[:c.latIdx.Load()]
	sortAsc(measured)
	emit(consumeReport{
		Consumed:   uint64(c.consumed.Load()),
		Rate:       float64(c.consumed.Load()) / duration.Seconds(),
		E2ENs:      summarize(measured),
		Duplicates: c.dupes,
	})
	return nil
}
