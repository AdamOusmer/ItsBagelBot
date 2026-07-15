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
	channels     int
	minRoutines  int
	maxRoutines  int
	minConsumers int
	maxConsumers int
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
}

type result struct {
	Node              string  `json:"node"`
	Server            string  `json:"server"`
	Messages          int     `json:"messages"`
	Channels          int     `json:"channels"`
	Processed         int64   `json:"processed"`
	UniqueProcessed   int64   `json:"unique_processed"`
	ProcessErrors     int64   `json:"process_errors"`
	OutputAccepted    int64   `json:"output_accepted"`
	OutputStored      uint64  `json:"output_stored"`
	InputStored       uint64  `json:"input_stored"`
	StreamStored      uint64  `json:"stream_stored"`
	OutputErrors      int64   `json:"output_errors"`
	OutputDuplicates  uint64  `json:"output_duplicates"`
	DurationMS        int64   `json:"duration_ms"`
	MessagesPerSecond float64 `json:"messages_per_second"`
	EngineDurationMS  int64   `json:"engine_duration_ms"`
	EnginePerSecond   float64 `json:"engine_messages_per_second"`
	ProcessP50US      float64 `json:"process_p50_us"`
	ProcessP95US      float64 `json:"process_p95_us"`
	ProcessP99US      float64 `json:"process_p99_us"`
	MaxInflight       int64   `json:"max_inflight"`
	GOMAXPROCS        int     `json:"gomaxprocs"`
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

type measuredFleetPublisher struct {
	bus.Publisher
	accepted atomic.Int64
	errors   atomic.Int64
}

type measuredPublisher interface {
	bus.Publisher
	counts() (int64, int64, uint64)
}

type memoryPublisher struct {
	accepted atomic.Int64
}

func (p *memoryPublisher) PublishOwned(context.Context, string, []byte) error {
	p.accepted.Add(1)
	return nil
}
func (p *memoryPublisher) PublishOwnedWithID(ctx context.Context, subject, _ string, payload []byte) error {
	return p.PublishOwned(ctx, subject, payload)
}
func (*memoryPublisher) Flush(context.Context) error      { return nil }
func (*memoryPublisher) Close() error                     { return nil }
func (p *memoryPublisher) counts() (int64, int64, uint64) { return p.accepted.Load(), 0, 0 }

func (p *measuredFleetPublisher) PublishOwned(ctx context.Context, subject string, payload []byte) error {
	if err := p.Publisher.PublishOwned(ctx, subject, payload); err != nil {
		p.errors.Add(1)
		return err
	}
	p.accepted.Add(1)
	return nil
}

func (p *measuredFleetPublisher) PublishOwnedWithID(ctx context.Context, subject, id string, payload []byte) error {
	if err := p.Publisher.PublishOwnedWithID(ctx, subject, id, payload); err != nil {
		p.errors.Add(1)
		return err
	}
	p.accepted.Add(1)
	return nil
}

func (p *measuredFleetPublisher) counts() (int64, int64, uint64) {
	var duplicates uint64
	if counter, ok := p.Publisher.(interface{ DuplicateCount() uint64 }); ok {
		duplicates = counter.DuplicateCount()
	}
	return p.accepted.Load(), p.errors.Load(), duplicates
}

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
	cfg := config{}
	bindFlags(&cfg)
	flag.Parse()
	if err := finalizeConfig(&cfg); err != nil {
		panic(err)
	}
	return cfg
}

func bindFlags(cfg *config) {
	flag.StringVar(&cfg.url, "url", "tls://nats:4222", "production NATS URL")
	flag.StringVar(&cfg.outputURL, "output-url", "", "output JetStream URL (defaults to -url)")
	flag.StringVar(&cfg.outputMode, "output", "nats", "output sink: nats or memory")
	flag.StringVar(&cfg.domain, "domain", "hub", "JetStream domain")
	flag.IntVar(&cfg.messages, "messages", 50_000, "commands to process")
	flag.IntVar(&cfg.channels, "channels", 1, "distinct broadcaster partitions")
	flag.IntVar(&cfg.minRoutines, "min-routines", 100, "initial routines per consumer")
	flag.IntVar(&cfg.maxRoutines, "max-routines", 100, "maximum routines per consumer")
	flag.IntVar(&cfg.minConsumers, "min-consumers", 1, "initial consumer units")
	flag.IntVar(&cfg.maxConsumers, "max-consumers", 4, "maximum consumer units")
	flag.DurationVar(&cfg.timeout, "timeout", 2*time.Minute, "test deadline")
}

func finalizeConfig(cfg *config) error {
	if cfg.outputURL == "" {
		cfg.outputURL = cfg.url
	}
	switch cfg.outputMode {
	case "nats", "memory":
	default:
		return errors.New("output must be nats or memory")
	}
	if err := validateLimits(*cfg); err != nil {
		return err
	}
	setRunIdentity(cfg)
	return loadCredentials(cfg)
}

func validateLimits(cfg config) error {
	if cfg.messages < 1 {
		return errors.New("messages must be positive")
	}
	if cfg.channels < 1 {
		return errors.New("channels must be positive")
	}
	if cfg.minRoutines < 1 {
		return errors.New("minimum routines must be positive")
	}
	if cfg.maxRoutines < cfg.minRoutines {
		return errors.New("invalid routine limits")
	}
	if cfg.minConsumers < 1 {
		return errors.New("minimum consumers must be positive")
	}
	if cfg.maxConsumers < cfg.minConsumers {
		return errors.New("invalid consumer limits")
	}
	return nil
}

func setRunIdentity(cfg *config) {
	runID := strings.ToLower(time.Now().UTC().Format("20060102t150405"))
	cfg.stream = "SESAME_BENCH_" + strings.ToUpper(strings.ReplaceAll(runID, "-", "_")) + "_" + strings.ToUpper(nuid.Next()[:6])
	prefix := "twitch.outgress.bench.sesame." + runID + "." + strings.ToLower(nuid.Next()[:6])
	cfg.input = prefix + ".input"
	cfg.output = prefix + ".output"
	cfg.group = "sesame_bench_" + runID + "_" + strings.ToLower(nuid.Next()[:6])
}

func loadCredentials(cfg *config) error {
	cfg.user = os.Getenv("NATS_USER")
	cfg.password = os.Getenv("NATS_PASSWORD")
	cfg.subUser = os.Getenv("NATS_SUB_USER")
	cfg.subPassword = os.Getenv("NATS_SUB_PASSWORD")
	if cfg.subUser == "" {
		cfg.subUser = cfg.user
		cfg.subPassword = cfg.password
	}
	cfg.caFile = os.Getenv("NATS_CA")
	if cfg.user == "" {
		return errors.New("publisher NATS user is required")
	}
	if cfg.password == "" {
		return errors.New("publisher NATS password is required")
	}
	if cfg.subUser == "" {
		return errors.New("subscriber NATS user is required")
	}
	if cfg.subPassword == "" {
		return errors.New("subscriber NATS password is required")
	}
	return nil
}

func run(cfg config) (r result, returnErr error) {
	r = initialResult(cfg)
	log := zap.NewNop()
	setup, js, err := createIsolatedStream(cfg)
	if err != nil {
		return r, err
	}
	defer setup.Close()
	r.Server = setup.ConnectedServerName()
	defer func() {
		if err := js.DeleteStream(cfg.stream); err != nil && returnErr == nil {
			returnErr = fmt.Errorf("delete isolated stream: %w", err)
		}
	}()

	if err := prefill(cfg, js); err != nil {
		return r, err
	}

	output, err := openOutputPublisher(cfg, log)
	if err != nil {
		return r, err
	}
	defer output.Close() //nolint:errcheck -- explicit flush below reports failures

	pipe := newPipeline(cfg, output, log)
	defer pipe.Close()

	sub, err := openInputSubscriber(cfg, log)
	if err != nil {
		return r, err
	}
	defer sub.Close() //nolint:errcheck

	run := benchmarkRun{cfg: cfg, js: js, output: output, pipe: pipe, sub: sub, result: &r, log: log}
	if err := run.execute(); err != nil {
		return r, err
	}
	return r, nil
}

type benchmarkMetrics struct {
	processed       atomic.Int64
	uniqueProcessed atomic.Int64
	processErrors   atomic.Int64
	inflight        atomic.Int64
	maxInflight     atomic.Int64
	seen            sync.Map
	latencies       *latencySamples
}

type benchmarkRun struct {
	cfg    config
	js     nats.JetStreamContext
	output measuredPublisher
	pipe   *engine.Pipeline
	sub    bus.Subscriber
	result *result
	log    *zap.Logger
}

func initialResult(cfg config) result {
	return result{
		Node: os.Getenv("NODE_NAME"), Messages: cfg.messages, Channels: cfg.channels, GOMAXPROCS: runtime.GOMAXPROCS(0),
		MinRoutines: cfg.minRoutines, MaxRoutines: cfg.maxRoutines,
		MinConsumers: cfg.minConsumers, MaxConsumers: cfg.maxConsumers,
	}
}

func createIsolatedStream(cfg config) (*nats.Conn, nats.JetStreamContext, error) {
	setup, err := connect(cfg, "sesame-bench-setup")
	if err != nil {
		return nil, nil, err
	}
	js, err := setup.JetStream(nats.Domain(cfg.domain), nats.MaxWait(5*time.Second), nats.PublishAsyncMaxPending(65_536))
	if err != nil {
		setup.Close()
		return nil, nil, err
	}
	_, err = js.AddStream(&nats.StreamConfig{
		Name: cfg.stream, Subjects: []string{cfg.input, cfg.output}, Storage: nats.MemoryStorage,
		Replicas: 1, MaxAge: 5 * time.Minute, MaxMsgs: int64(cfg.messages*3 + 1000), Discard: nats.DiscardOld,
	})
	if err != nil {
		setup.Close()
		return nil, nil, fmt.Errorf("create isolated stream: %w", err)
	}
	return setup, js, nil
}

func openOutputPublisher(cfg config, log *zap.Logger) (measuredPublisher, error) {
	if cfg.outputMode == "memory" {
		return &memoryPublisher{}, nil
	}
	fleet, err := bus.NewPublisherForStream(cfg.outputURL, cfg.stream, log)
	if err != nil {
		return nil, fmt.Errorf("open output publisher: %w", err)
	}
	return &measuredFleetPublisher{Publisher: fleet}, nil
}

func newPipeline(cfg config, output measuredPublisher, log *zap.Logger) *engine.Pipeline {
	return engine.NewPipeline(engine.Deps{
		Proj: benchReader{}, Live: liveAlways{}, Cooldown: engine.NoopCooldown{},
		Pub: output, Log: log, Automod: automod.New(),
	}, engine.NewRegistry(log), engine.Config{
		OutgressPremium: cfg.output, OutgressStandard: cfg.output,
	})
}

func openInputSubscriber(cfg config, log *zap.Logger) (bus.Subscriber, error) {
	// NewLaneSubscriber reads credentials from the environment. Switch them only
	// while opening this connection; existing publisher connections are unaffected.
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

func (b benchmarkRun) execute() error {
	cfg := b.cfg
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	gate := make(chan struct{})
	metrics := &benchmarkMetrics{latencies: &latencySamples{values: make([]time.Duration, 0, cfg.messages/100+1)}}
	handle := metrics.handler(gate, b.pipe)
	weighted, err := bus.ConsumeWeighted(ctx, nil, []bus.WeightedLane{{
		Sub: b.sub, Subject: cfg.input, Handle: handle,
	}}, benchmarkScalePolicy(cfg), b.log)
	if err != nil {
		return fmt.Errorf("start weighted consumer: %w", err)
	}

	time.Sleep(250 * time.Millisecond)
	started := time.Now()
	close(gate)
	if err := waitForInputs(cfg, metrics); err != nil {
		return err
	}
	engineElapsed := time.Since(started)
	recordEngineResult(b.result, metrics, b.output, engineElapsed)
	cancel()
	if err := drainBenchmark(weighted, b.output); err != nil {
		b.result.DurationMS = time.Since(started).Milliseconds()
		return err
	}
	elapsed := time.Since(started)
	b.result.DurationMS = elapsed.Milliseconds()
	_, b.result.OutputErrors, b.result.OutputDuplicates = b.output.counts()
	if err := inspectStoredMessages(cfg, b.js, b.result); err != nil {
		return err
	}
	b.result.MessagesPerSecond = float64(cfg.messages) / elapsed.Seconds()
	return validateResult(cfg, *b.result)
}

func benchmarkScalePolicy(cfg config) bus.ScalePolicy {
	return bus.ScalePolicy{
		MinRoutines: cfg.minRoutines, MaxRoutines: cfg.maxRoutines,
		MinConsumers: cfg.minConsumers, MaxConsumers: cfg.maxConsumers,
		ScaleUpAfter: 2 * time.Second, ScaleDownAfter: 45 * time.Second,
	}
}

func (m *benchmarkMetrics) handler(gate <-chan struct{}, pipe *engine.Pipeline) func(*bus.Message) error {
	return func(msg *bus.Message) error {
		<-gate
		current := m.inflight.Add(1)
		m.observeInflight(current)
		started := time.Now()
		err := pipe.Process(msg)
		m.inflight.Add(-1)
		if err != nil {
			m.processErrors.Add(1)
			return err
		}
		sequence := m.processed.Add(1)
		if _, loaded := m.seen.LoadOrStore(msg.UUID, struct{}{}); !loaded {
			m.uniqueProcessed.Add(1)
		}
		m.latencies.add(sequence, time.Since(started))
		return nil
	}
}

func (m *benchmarkMetrics) observeInflight(current int64) {
	for maximum := m.maxInflight.Load(); current > maximum; maximum = m.maxInflight.Load() {
		if m.maxInflight.CompareAndSwap(maximum, current) {
			return
		}
	}
}

func waitForInputs(cfg config, metrics *benchmarkMetrics) error {
	deadline := time.NewTimer(cfg.timeout)
	defer deadline.Stop()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for metrics.uniqueProcessed.Load() < int64(cfg.messages) {
		select {
		case <-deadline.C:
			return fmt.Errorf("timeout after processing %d/%d unique inputs (%d attempts)", metrics.uniqueProcessed.Load(), cfg.messages, metrics.processed.Load())
		case <-ticker.C:
		}
	}
	return nil
}

func recordEngineResult(r *result, metrics *benchmarkMetrics, output measuredPublisher, elapsed time.Duration) {
	r.Processed = metrics.processed.Load()
	r.UniqueProcessed = metrics.uniqueProcessed.Load()
	r.ProcessErrors = metrics.processErrors.Load()
	r.OutputAccepted, r.OutputErrors, r.OutputDuplicates = output.counts()
	r.EngineDurationMS = elapsed.Milliseconds()
	r.EnginePerSecond = float64(r.Processed) / elapsed.Seconds()
	r.MaxInflight = metrics.maxInflight.Load()
	r.ProcessP50US, r.ProcessP95US, r.ProcessP99US = percentiles(metrics.latencies.values)
}

func drainBenchmark(weighted *bus.Weighted, output measuredPublisher) error {
	drainCtx, drainCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer drainCancel()
	if err := weighted.Drain(drainCtx); err != nil {
		return fmt.Errorf("drain input: %w", err)
	}
	flushCtx, flushCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer flushCancel()
	if err := output.Flush(flushCtx); err != nil {
		return fmt.Errorf("flush output: %w", err)
	}
	return nil
}

func inspectStoredMessages(cfg config, js nats.JetStreamContext, r *result) error {
	deadline := time.Now().Add(5 * time.Second)
	for {
		info, err := js.StreamInfo(cfg.stream, &nats.StreamInfoRequest{SubjectsFilter: ">"})
		if err != nil {
			return fmt.Errorf("inspect unique outputs: %w", err)
		}
		r.OutputStored = info.State.Subjects[cfg.output]
		r.InputStored = info.State.Subjects[cfg.input]
		r.StreamStored = info.State.Msgs
		if r.OutputStored >= uint64(cfg.messages) || time.Now().After(deadline) {
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func validateResult(cfg config, r result) error {
	if r.ProcessErrors != 0 {
		return errors.New("engine processing errors")
	}
	if r.OutputErrors != 0 {
		return errors.New("output publishing errors")
	}
	if r.UniqueProcessed != int64(cfg.messages) {
		return errors.New("unique input count mismatch")
	}
	if r.OutputAccepted < int64(cfg.messages) {
		return errors.New("accepted output count mismatch")
	}
	if cfg.outputMode != "nats" {
		return nil
	}
	if r.OutputStored != uint64(cfg.messages) {
		return errors.New("stored output count mismatch")
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
		broadcasterID := fmt.Sprintf("18446744073709%05d", i%cfg.channels)
		body, err := sonic.ConfigFastest.Marshal(map[string]any{
			"type": "channel.chat.message", "lane": "standard", "event_id": id,
			"broadcaster_user_id": broadcasterID, "broadcaster_user_login": "benchmark",
			"broadcaster_user_name": "Benchmark", "chatter_user_id": fmt.Sprint(i + 1),
			"chatter_user_login": "viewer", "chatter_user_name": "Viewer", "text": "!bench", "msg_id": id,
		})
		if err != nil {
			return err
		}
		msg := nats.NewMsg(cfg.input)
		msg.Data = body
		msg.Header.Set(bus.MessageIDHeader, id)
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
