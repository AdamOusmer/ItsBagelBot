// Command sesame-live-acceptance measures one production-shaped Sesame pod
// without touching production lane consumers or Twitch outgress. It creates a
// temporary memory stream on twitch.outgress.bench.*, preloads realistic custom
// command events, drains them through the real weighted consumer and engine
// Pipeline, persists emitted actions back into the isolated stream, and removes
// the stream on exit.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"ItsBagelBot/app/sesame/automod"
	"ItsBagelBot/app/sesame/engine"
	"ItsBagelBot/internal/projection"
	"ItsBagelBot/pkg/bus"
	pkgvalkey "ItsBagelBot/pkg/valkey"

	wmnats "github.com/ThreeDotsLabs/watermill-nats/v2/pkg/nats"
	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/bytedance/sonic"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nuid"
	"go.uber.org/zap"
)

type config struct {
	url          string
	outputURL    string
	outputMode   string
	domain       string
	messages     int
	minRoutines  int
	maxRoutines  int
	minConsumers int
	maxConsumers int
	dedup        string
	timeout      time.Duration
	stream       string
	input        string
	output       string
	group        string
	user         string
	password     string
	subUser      string
	subPassword  string
	caFile       string
	valkeyAddr   string
	valkeyPass   string
}

type result struct {
	Node              string  `json:"node"`
	Server            string  `json:"server"`
	Messages          int     `json:"messages"`
	Processed         int64   `json:"processed"`
	ProcessErrors     int64   `json:"process_errors"`
	OutputAccepted    int64   `json:"output_accepted"`
	OutputErrors      int64   `json:"output_errors"`
	DurationMS        int64   `json:"duration_ms"`
	MessagesPerSecond float64 `json:"messages_per_second"`
	EngineDurationMS  int64   `json:"engine_duration_ms"`
	EnginePerSecond   float64 `json:"engine_messages_per_second"`
	ProcessP50US      float64 `json:"process_p50_us"`
	ProcessP95US      float64 `json:"process_p95_us"`
	ProcessP99US      float64 `json:"process_p99_us"`
	MaxInflight       int64   `json:"max_inflight"`
	GOMAXPROCS        int     `json:"gomaxprocs"`
	Dedup             string  `json:"dedup"`
	MinRoutines       int     `json:"min_routines"`
	MaxRoutines       int     `json:"max_routines"`
	MinConsumers      int     `json:"min_consumers"`
	MaxConsumers      int     `json:"max_consumers"`
	Passed            bool    `json:"passed"`
	Failure           string  `json:"failure,omitempty"`
}

type benchReader struct{}

func (benchReader) User(context.Context, uint64) (projection.User, error) {
	return projection.User{Locale: "en"}, nil
}
func (benchReader) Modules(context.Context, uint64) ([]projection.ModuleView, error) {
	return nil, nil
}
func (benchReader) Command(_ context.Context, _ uint64, name string) (projection.Command, bool, error) {
	if name != "bench" {
		return projection.Command{}, false, nil
	}
	return projection.Command{
		Name: "bench", Response: "Hello {user}, command received", IsActive: true,
	}, true, nil
}

type liveAlways struct{}

func (liveAlways) IsLive(context.Context, uint64) (bool, error) { return true, nil }
func (liveAlways) SetLive(context.Context, uint64) error        { return nil }
func (liveAlways) ClearLive(context.Context, uint64) error      { return nil }

// jsPublisher implements the fleet Publisher contract on the isolated stream.
// Production is still NATS 2.11, so this intentionally measures its supported
// per-message async PubAck path rather than pretending Fast-Ingest is available.
type jsPublisher struct {
	nc       *nats.Conn
	js       nats.JetStreamContext
	subject  string
	accepted atomic.Int64
	errors   atomic.Int64
}

type measuredPublisher interface {
	bus.Publisher
	counts() (int64, int64)
}

type memoryPublisher struct {
	accepted atomic.Int64
}

func (p *memoryPublisher) PublishOwned(context.Context, string, []byte) error {
	p.accepted.Add(1)
	return nil
}
func (*memoryPublisher) Flush(context.Context) error { return nil }
func (*memoryPublisher) Close() error                { return nil }
func (p *memoryPublisher) counts() (int64, int64)    { return p.accepted.Load(), 0 }

func newJSPublisher(cfg config) (*jsPublisher, error) {
	nc, err := connectURL(cfg, cfg.outputURL, "sesame-bench-output")
	if err != nil {
		return nil, err
	}
	p := &jsPublisher{nc: nc, subject: cfg.output}
	js, err := nc.JetStream(
		nats.Domain(cfg.domain),
		nats.PublishAsyncMaxPending(65_536),
		nats.PublishAsyncErrHandler(func(_ nats.JetStream, _ *nats.Msg, _ error) {
			p.errors.Add(1)
		}),
	)
	if err != nil {
		nc.Close()
		return nil, err
	}
	p.js = js
	return p, nil
}

func (p *jsPublisher) PublishOwned(_ context.Context, _ string, payload []byte) error {
	msg := nats.NewMsg(p.subject)
	msg.Data = payload
	msg.Header.Set(nats.MsgIdHdr, nuid.Next())
	if _, err := p.js.PublishMsgAsync(msg); err != nil {
		p.errors.Add(1)
		return err
	}
	p.accepted.Add(1)
	return nil
}

func (p *jsPublisher) Flush(ctx context.Context) error {
	select {
	case <-p.js.PublishAsyncComplete():
		if count := p.errors.Load(); count != 0 {
			return fmt.Errorf("%d asynchronous output publishes failed", count)
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (p *jsPublisher) Close() error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	err := p.Flush(ctx)
	cancel()
	if drainErr := p.nc.Drain(); err == nil {
		err = drainErr
	}
	return err
}

func (p *jsPublisher) counts() (int64, int64) { return p.accepted.Load(), p.errors.Load() }

type latencySamples struct {
	mu     sync.Mutex
	values []time.Duration
}

func (s *latencySamples) add(sequence int64, value time.Duration) {
	if sequence%100 != 0 {
		return
	}
	s.mu.Lock()
	s.values = append(s.values, value)
	s.mu.Unlock()
}

func main() {
	cfg := parseFlags()
	r, err := run(cfg)
	if err != nil {
		r.Failure = err.Error()
	} else {
		r.Passed = true
	}
	body, marshalErr := json.MarshalIndent(r, "", "  ")
	if marshalErr != nil {
		panic(marshalErr)
	}
	fmt.Println(string(body))
	if err != nil {
		os.Exit(1)
	}
}

func parseFlags() config {
	runID := strings.ToLower(time.Now().UTC().Format("20060102t150405"))
	cfg := bindFlags()
	flag.Parse()
	applyConfigDefaults(&cfg)
	applyConfigEnvironment(&cfg)
	validateConfig(cfg)
	cfg.stream = "SESAME_BENCH_" + strings.ToUpper(strings.ReplaceAll(runID, "-", "_")) + "_" + strings.ToUpper(nuid.Next()[:6])
	prefix := "twitch.outgress.bench.sesame." + runID + "." + strings.ToLower(nuid.Next()[:6])
	cfg.input = prefix + ".input"
	cfg.output = prefix + ".output"
	cfg.group = "sesame_bench_" + runID + "_" + strings.ToLower(nuid.Next()[:6])
	return cfg
}

func bindFlags() config {
	cfg := config{}
	flag.StringVar(&cfg.url, "url", "tls://nats:4222", "production NATS URL")
	flag.StringVar(&cfg.outputURL, "output-url", "", "output JetStream URL (defaults to -url)")
	flag.StringVar(&cfg.outputMode, "output", "nats", "output sink: nats or memory")
	flag.StringVar(&cfg.domain, "domain", "hub", "JetStream domain")
	flag.IntVar(&cfg.messages, "messages", 50_000, "commands to process")
	flag.IntVar(&cfg.minRoutines, "min-routines", 50, "initial routines per consumer")
	flag.IntVar(&cfg.maxRoutines, "max-routines", 200, "maximum routines per consumer")
	flag.IntVar(&cfg.minConsumers, "min-consumers", 1, "initial consumer units")
	flag.IntVar(&cfg.maxConsumers, "max-consumers", 4, "maximum consumer units")
	flag.StringVar(&cfg.dedup, "dedup", "valkey", "dedup backend: valkey or noop")
	flag.DurationVar(&cfg.timeout, "timeout", 2*time.Minute, "test deadline")
	return cfg
}

func applyConfigDefaults(cfg *config) {
	if cfg.outputURL == "" {
		cfg.outputURL = cfg.url
	}
}

func applyConfigEnvironment(cfg *config) {
	cfg.user = os.Getenv("NATS_USER")
	cfg.password = os.Getenv("NATS_PASSWORD")
	cfg.subUser = os.Getenv("NATS_SUB_USER")
	cfg.subPassword = os.Getenv("NATS_SUB_PASSWORD")
	if cfg.subUser == "" {
		cfg.subUser = cfg.user
		cfg.subPassword = cfg.password
	}
	cfg.caFile = os.Getenv("NATS_CA")
	cfg.valkeyAddr = os.Getenv("VALKEY_ADDR")
	cfg.valkeyPass = os.Getenv("VALKEY_PASSWORD")
}

func validateConfig(cfg config) {
	validateChoice(cfg.outputMode, "output", "nats", "memory")
	validateChoice(cfg.dedup, "dedup", "valkey", "noop")
	requirePositive(cfg.messages, "messages")
	requirePositive(cfg.minRoutines, "min-routines")
	requireAtLeast(cfg.maxRoutines, cfg.minRoutines, "max-routines")
	requirePositive(cfg.minConsumers, "min-consumers")
	requireAtLeast(cfg.maxConsumers, cfg.minConsumers, "max-consumers")
	requireText(cfg.user, "NATS_USER")
	requireText(cfg.password, "NATS_PASSWORD")
	requireText(cfg.subUser, "NATS_SUB_USER")
	requireText(cfg.subPassword, "NATS_SUB_PASSWORD")
	if cfg.dedup == "valkey" {
		requireText(cfg.valkeyAddr, "VALKEY_ADDR")
		requireText(cfg.valkeyPass, "VALKEY_PASSWORD")
	}
}

func validateChoice(value, name, first, second string) {
	if value == first {
		return
	}
	if value == second {
		return
	}
	panic(name + " must be " + first + " or " + second)
}

func requirePositive(value int, name string) {
	if value < 1 {
		panic(name + " must be positive")
	}
}

func requireAtLeast(value, minimum int, name string) {
	if value < minimum {
		panic(name + " is below its minimum")
	}
}

func requireText(value, name string) {
	if value == "" {
		panic(name + " is required")
	}
}

func run(cfg config) (r result, returnErr error) {
	r = initialResult(cfg)
	log := zap.NewNop()
	fixture, err := newStreamFixture(cfg)
	if err != nil {
		return r, err
	}
	r.Server = fixture.nc.ConnectedServerName()
	defer func() {
		if err := fixture.Close(); err != nil && returnErr == nil {
			returnErr = err
		}
	}()
	if err := prefill(cfg, fixture.js); err != nil {
		return r, err
	}
	output, err := newMeasuredPublisher(cfg)
	if err != nil {
		return r, err
	}
	defer output.Close() //nolint:errcheck -- explicit flush below reports failures
	dedup, err := newDedupFixture(cfg)
	if err != nil {
		return r, err
	}
	defer dedup.Close()
	pipe := engine.NewPipeline(engine.Deps{
		Proj: benchReader{}, Live: liveAlways{}, Cooldown: engine.NoopCooldown{}, Dedup: dedup.store,
		Pub: output, Log: log, Automod: automod.New(),
	}, engine.NewRegistry(log), engine.Config{
		OutgressPremium: cfg.output, OutgressStandard: cfg.output,
	})
	defer pipe.Close()
	sub, err := newBenchmarkSubscriber(cfg, log)
	if err != nil {
		return r, err
	}
	defer sub.Close() //nolint:errcheck
	execution := pipelineExecution{cfg: cfg, pipe: pipe, sub: sub, output: output, log: log}
	metrics, err := executePipeline(execution)
	metrics.apply(&r)
	if err != nil {
		return r, err
	}
	return r, validateResult(cfg, r)
}

func initialResult(cfg config) result {
	return result{
		Node: os.Getenv("NODE_NAME"), Messages: cfg.messages, GOMAXPROCS: runtime.GOMAXPROCS(0),
		Dedup: cfg.dedup, MinRoutines: cfg.minRoutines, MaxRoutines: cfg.maxRoutines,
		MinConsumers: cfg.minConsumers, MaxConsumers: cfg.maxConsumers,
	}
}

type streamFixture struct {
	cfg config
	nc  *nats.Conn
	js  nats.JetStreamContext
}

func newStreamFixture(cfg config) (*streamFixture, error) {
	nc, err := connect(cfg, "sesame-bench-setup")
	if err != nil {
		return nil, err
	}
	js, err := nc.JetStream(nats.Domain(cfg.domain), nats.MaxWait(5*time.Second), nats.PublishAsyncMaxPending(65_536))
	if err != nil {
		nc.Close()
		return nil, err
	}
	_, err = js.AddStream(&nats.StreamConfig{
		Name: cfg.stream, Subjects: []string{cfg.input, cfg.output}, Storage: nats.MemoryStorage,
		Replicas: 1, MaxAge: 5 * time.Minute, MaxMsgs: int64(cfg.messages*3 + 1000), Discard: nats.DiscardOld,
	})
	if err != nil {
		nc.Close()
		return nil, fmt.Errorf("create isolated stream: %w", err)
	}
	return &streamFixture{cfg: cfg, nc: nc, js: js}, nil
}

func (f *streamFixture) Close() error {
	defer f.nc.Close()
	if err := f.js.DeleteStream(f.cfg.stream); err != nil {
		return fmt.Errorf("delete isolated stream: %w", err)
	}
	return nil
}

func newMeasuredPublisher(cfg config) (measuredPublisher, error) {
	if cfg.outputMode == "memory" {
		return &memoryPublisher{}, nil
	}
	output, err := newJSPublisher(cfg)
	if err != nil {
		return nil, fmt.Errorf("open output publisher: %w", err)
	}
	return output, nil
}

type dedupFixture struct {
	store engine.DedupStore
	close func()
}

func newDedupFixture(cfg config) (dedupFixture, error) {
	if cfg.dedup == "noop" {
		return dedupFixture{store: engine.NoopDedup{}, close: func() {}}, nil
	}
	vc, err := pkgvalkey.NewClient(cfg.valkeyAddr, cfg.valkeyPass)
	if err != nil {
		return dedupFixture{}, fmt.Errorf("connect valkey: %w", err)
	}
	store := engine.NewBatchedValkeyDedup(vc, 2*time.Minute, 128, 200*time.Microsecond)
	return dedupFixture{store: store, close: func() { store.Close(); vc.Close() }}, nil
}

func (d dedupFixture) Close() { d.close() }

func newBenchmarkSubscriber(cfg config, log *zap.Logger) (message.Subscriber, error) {
	// NewLaneSubscriber reads the fleet credential contract from the environment.
	publishUser, publishPassword := os.Getenv("NATS_USER"), os.Getenv("NATS_PASSWORD")
	defer func() {
		_ = os.Setenv("NATS_USER", publishUser)
		_ = os.Setenv("NATS_PASSWORD", publishPassword)
	}()
	_ = os.Setenv("NATS_USER", cfg.subUser)
	_ = os.Setenv("NATS_PASSWORD", cfg.subPassword)
	sub, err := bus.NewLaneSubscriber(bus.LaneConfig{
		URL: cfg.url, Stream: cfg.stream, Subject: cfg.input, Group: cfg.group,
		NakDelay: time.Second, MaxRedeliveries: 2,
	}, log)
	if err != nil {
		return nil, fmt.Errorf("open isolated subscriber: %w", err)
	}
	return sub, nil
}

type pipelineMetrics struct {
	processed, processErrors, outputAccepted, outputErrors int64
	engineElapsed, elapsed                                 time.Duration
	maxInflight                                            int64
	latencies                                              []time.Duration
}

func (m pipelineMetrics) apply(r *result) {
	r.Processed, r.ProcessErrors = m.processed, m.processErrors
	r.OutputAccepted, r.OutputErrors = m.outputAccepted, m.outputErrors
	r.EngineDurationMS = m.engineElapsed.Milliseconds()
	r.EnginePerSecond = float64(m.processed) / m.engineElapsed.Seconds()
	r.DurationMS = m.elapsed.Milliseconds()
	r.MessagesPerSecond = float64(m.processed) / m.elapsed.Seconds()
	r.MaxInflight = m.maxInflight
	r.ProcessP50US, r.ProcessP95US, r.ProcessP99US = percentiles(m.latencies)
}

type processor struct {
	pipe                                            *engine.Pipeline
	gate                                            chan struct{}
	processed, processErrors, inflight, maxInflight atomic.Int64
	latencies                                       *latencySamples
}

func (p *processor) handle(msg *message.Message) error {
	<-p.gate
	current := p.inflight.Add(1)
	p.observeInflight(current)
	started := time.Now()
	err := p.pipe.Process(msg)
	p.inflight.Add(-1)
	if err != nil {
		p.processErrors.Add(1)
		return err
	}
	sequence := p.processed.Add(1)
	p.latencies.add(sequence, time.Since(started))
	return nil
}

func (p *processor) observeInflight(current int64) {
	for maximum := p.maxInflight.Load(); current > maximum; maximum = p.maxInflight.Load() {
		if p.maxInflight.CompareAndSwap(maximum, current) {
			return
		}
	}
}

type pipelineExecution struct {
	cfg    config
	pipe   *engine.Pipeline
	sub    message.Subscriber
	output measuredPublisher
	log    *zap.Logger
}

func executePipeline(execution pipelineExecution) (pipelineMetrics, error) {
	cfg := execution.cfg
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p := &processor{pipe: execution.pipe, gate: make(chan struct{}), latencies: &latencySamples{values: make([]time.Duration, 0, cfg.messages/100+1)}}
	weighted, err := startWeighted(execution, ctx, p.handle)
	if err != nil {
		return pipelineMetrics{}, err
	}
	time.Sleep(250 * time.Millisecond)
	started := time.Now()
	close(p.gate)
	if err := waitUntilProcessed(cfg, &p.processed); err != nil {
		return pipelineMetrics{}, err
	}
	engineElapsed := time.Since(started)
	accepted, outputErrors := execution.output.counts()
	cancel()
	if err := drainWeighted(weighted); err != nil {
		return pipelineMetrics{}, err
	}
	if err := flushOutput(execution.output); err != nil {
		return pipelineMetrics{}, err
	}
	_, outputErrors = execution.output.counts()
	return pipelineMetrics{
		processed: p.processed.Load(), processErrors: p.processErrors.Load(),
		outputAccepted: accepted, outputErrors: outputErrors,
		engineElapsed: engineElapsed, elapsed: time.Since(started),
		maxInflight: p.maxInflight.Load(), latencies: p.latencies.values,
	}, nil
}

func startWeighted(execution pipelineExecution, ctx context.Context, handle func(*message.Message) error) (*bus.Weighted, error) {
	cfg := execution.cfg
	weighted, err := bus.ConsumeWeighted(ctx, nil, []bus.WeightedLane{{Sub: execution.sub, Subject: cfg.input, Handle: handle}}, bus.ScalePolicy{
		MinRoutines: cfg.minRoutines, MaxRoutines: cfg.maxRoutines,
		MinConsumers: cfg.minConsumers, MaxConsumers: cfg.maxConsumers,
		ScaleUpAfter: 2 * time.Second, ScaleDownAfter: 45 * time.Second,
	}, execution.log)
	if err != nil {
		return nil, fmt.Errorf("start weighted consumer: %w", err)
	}
	return weighted, nil
}

func waitUntilProcessed(cfg config, processed *atomic.Int64) error {
	deadline := time.NewTimer(cfg.timeout)
	defer deadline.Stop()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for processed.Load() < int64(cfg.messages) {
		select {
		case <-deadline.C:
			return fmt.Errorf("timeout after processing %d/%d", processed.Load(), cfg.messages)
		case <-ticker.C:
		}
	}
	return nil
}

func drainWeighted(weighted *bus.Weighted) error {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := weighted.Drain(ctx); err != nil {
		return fmt.Errorf("drain input: %w", err)
	}
	return nil
}

func flushOutput(output measuredPublisher) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := output.Flush(ctx); err != nil {
		return fmt.Errorf("flush output: %w", err)
	}
	return nil
}

func validateResult(cfg config, r result) error {
	if r.ProcessErrors != 0 {
		return errors.New("processing error count mismatch")
	}
	if r.OutputErrors != 0 {
		return errors.New("output error count mismatch")
	}
	if r.Processed != int64(cfg.messages) {
		return errors.New("processed message count mismatch")
	}
	if r.OutputAccepted != int64(cfg.messages) {
		return errors.New("accepted output count mismatch")
	}
	return nil
}

func connect(cfg config, name string) (*nats.Conn, error) {
	return connectURL(cfg, cfg.url, name)
}

func connectURL(cfg config, url, name string) (*nats.Conn, error) {
	opts := []nats.Option{nats.Name(name), nats.UserInfo(cfg.user, cfg.password), nats.Timeout(5 * time.Second)}
	if cfg.caFile != "" {
		opts = append(opts, nats.RootCAs(cfg.caFile))
	}
	return nats.Connect(url, opts...)
}

func prefill(cfg config, js nats.JetStreamContext) error {
	for i := 0; i < cfg.messages; i++ {
		id := fmt.Sprintf("%s-%d", cfg.group, i)
		body, err := sonic.ConfigFastest.Marshal(map[string]any{
			"type": "channel.chat.message", "lane": "standard", "event_id": id,
			"broadcaster_user_id": "18446744073709550000", "broadcaster_user_login": "benchmark",
			"broadcaster_user_name": "Benchmark", "chatter_user_id": fmt.Sprint(i + 1),
			"chatter_user_login": "viewer", "chatter_user_name": "Viewer", "text": "!bench", "msg_id": id,
		})
		if err != nil {
			return err
		}
		msg := nats.NewMsg(cfg.input)
		msg.Data = body
		msg.Header.Set(wmnats.WatermillUUIDHdr, id)
		msg.Header.Set(nats.MsgIdHdr, id)
		if _, err := js.PublishMsgAsync(msg); err != nil {
			return fmt.Errorf("prefill message %d: %w", i, err)
		}
	}
	select {
	case <-js.PublishAsyncComplete():
		return nil
	case <-time.After(30 * time.Second):
		return errors.New("prefill PubAck timeout")
	}
}

func percentiles(values []time.Duration) (float64, float64, float64) {
	if len(values) == 0 {
		return 0, 0, 0
	}
	sort.Slice(values, func(i, j int) bool { return values[i] < values[j] })
	at := func(p float64) float64 {
		index := int(float64(len(values)-1) * p)
		return float64(values[index]) / float64(time.Microsecond)
	}
	return at(0.50), at(0.95), at(0.99)
}
