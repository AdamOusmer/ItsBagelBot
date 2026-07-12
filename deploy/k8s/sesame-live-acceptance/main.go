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
	flag.Parse()
	if cfg.outputURL == "" {
		cfg.outputURL = cfg.url
	}
	if cfg.outputMode != "nats" && cfg.outputMode != "memory" {
		panic("output must be nats or memory")
	}
	if cfg.messages < 1 || cfg.minRoutines < 1 || cfg.maxRoutines < cfg.minRoutines || cfg.minConsumers < 1 || cfg.maxConsumers < cfg.minConsumers {
		panic("invalid message or routine limits")
	}
	if cfg.dedup != "valkey" && cfg.dedup != "noop" {
		panic("dedup must be valkey or noop")
	}
	cfg.stream = "SESAME_BENCH_" + strings.ToUpper(strings.ReplaceAll(runID, "-", "_")) + "_" + strings.ToUpper(nuid.Next()[:6])
	prefix := "twitch.outgress.bench.sesame." + runID + "." + strings.ToLower(nuid.Next()[:6])
	cfg.input = prefix + ".input"
	cfg.output = prefix + ".output"
	cfg.group = "sesame_bench_" + runID + "_" + strings.ToLower(nuid.Next()[:6])
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
	if cfg.user == "" || cfg.password == "" || cfg.subUser == "" || cfg.subPassword == "" {
		panic("publisher and subscriber NATS credentials are required")
	}
	if cfg.dedup == "valkey" && (cfg.valkeyAddr == "" || cfg.valkeyPass == "") {
		panic("VALKEY_ADDR and VALKEY_PASSWORD are required for valkey dedup")
	}
	return cfg
}

func run(cfg config) (r result, returnErr error) {
	r = result{
		Node: os.Getenv("NODE_NAME"), Messages: cfg.messages, GOMAXPROCS: runtime.GOMAXPROCS(0),
		Dedup: cfg.dedup, MinRoutines: cfg.minRoutines, MaxRoutines: cfg.maxRoutines,
		MinConsumers: cfg.minConsumers, MaxConsumers: cfg.maxConsumers,
	}
	log := zap.NewNop()
	setup, err := connect(cfg, "sesame-bench-setup")
	if err != nil {
		return r, err
	}
	defer setup.Close()
	r.Server = setup.ConnectedServerName()
	js, err := setup.JetStream(nats.Domain(cfg.domain), nats.MaxWait(5*time.Second), nats.PublishAsyncMaxPending(65_536))
	if err != nil {
		return r, err
	}
	if _, err := js.AddStream(&nats.StreamConfig{
		Name: cfg.stream, Subjects: []string{cfg.input, cfg.output}, Storage: nats.MemoryStorage,
		Replicas: 1, MaxAge: 5 * time.Minute, MaxMsgs: int64(cfg.messages*3 + 1000), Discard: nats.DiscardOld,
	}); err != nil {
		return r, fmt.Errorf("create isolated stream: %w", err)
	}
	defer func() {
		if err := js.DeleteStream(cfg.stream); err != nil && returnErr == nil {
			returnErr = fmt.Errorf("delete isolated stream: %w", err)
		}
	}()

	if err := prefill(cfg, js); err != nil {
		return r, err
	}

	var output measuredPublisher
	if cfg.outputMode == "memory" {
		output = &memoryPublisher{}
	} else {
		output, err = newJSPublisher(cfg)
		if err != nil {
			return r, fmt.Errorf("open output publisher: %w", err)
		}
	}
	defer output.Close() //nolint:errcheck -- explicit flush below reports failures

	var valkeyClient interface{ Close() }
	var dedup engine.DedupStore = engine.NoopDedup{}
	var batchedDedup *engine.BatchedValkeyDedup
	if cfg.dedup == "valkey" {
		vc, err := pkgvalkey.NewClient(cfg.valkeyAddr, cfg.valkeyPass)
		if err != nil {
			return r, fmt.Errorf("connect valkey: %w", err)
		}
		valkeyClient = vc
		defer valkeyClient.Close()
		batchedDedup = engine.NewBatchedValkeyDedup(vc, 2*time.Minute, 128, 200*time.Microsecond)
		defer batchedDedup.Close()
		dedup = batchedDedup
	}

	pipe := engine.NewPipeline(engine.Deps{
		Proj: benchReader{}, Live: liveAlways{}, Cooldown: engine.NoopCooldown{}, Dedup: dedup,
		Pub: output, Log: log, Automod: automod.New(),
	}, engine.NewRegistry(log), engine.Config{
		OutgressPremium: cfg.output, OutgressStandard: cfg.output,
	})
	defer pipe.Close()

	// bus.NewLaneSubscriber intentionally reads the fleet credential contract
	// from the environment. Switch only for this synchronous connection setup:
	// worker_bus publishes the isolated outgress subject while outgress_bus has
	// least-privilege subscribe rights to it. Existing connections are unaffected.
	publishUser, publishPassword := os.Getenv("NATS_USER"), os.Getenv("NATS_PASSWORD")
	_ = os.Setenv("NATS_USER", cfg.subUser)
	_ = os.Setenv("NATS_PASSWORD", cfg.subPassword)
	sub, err := bus.NewLaneSubscriber(bus.LaneConfig{
		URL: cfg.url, Stream: cfg.stream, Subject: cfg.input, Group: cfg.group,
		NakDelay: time.Second, MaxRedeliveries: 2,
	}, log)
	_ = os.Setenv("NATS_USER", publishUser)
	_ = os.Setenv("NATS_PASSWORD", publishPassword)
	if err != nil {
		return r, fmt.Errorf("open isolated subscriber: %w", err)
	}
	defer sub.Close() //nolint:errcheck

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	gate := make(chan struct{})
	var processed, processErrors, inflight, maxInflight atomic.Int64
	latencies := &latencySamples{values: make([]time.Duration, 0, cfg.messages/100+1)}
	handle := func(msg *message.Message) error {
		<-gate
		current := inflight.Add(1)
		for maximum := maxInflight.Load(); current > maximum && !maxInflight.CompareAndSwap(maximum, current); maximum = maxInflight.Load() {
		}
		started := time.Now()
		err := pipe.Process(msg)
		inflight.Add(-1)
		if err != nil {
			processErrors.Add(1)
			return err
		}
		sequence := processed.Add(1)
		latencies.add(sequence, time.Since(started))
		return nil
	}
	weighted, err := bus.ConsumeWeighted(ctx, nil, []bus.WeightedLane{{
		Sub: sub, Subject: cfg.input, Handle: handle,
	}}, bus.ScalePolicy{
		MinRoutines: cfg.minRoutines, MaxRoutines: cfg.maxRoutines,
		MinConsumers: cfg.minConsumers, MaxConsumers: cfg.maxConsumers,
		ScaleUpAfter: 2 * time.Second, ScaleDownAfter: 45 * time.Second,
	}, log)
	if err != nil {
		return r, fmt.Errorf("start weighted consumer: %w", err)
	}

	time.Sleep(250 * time.Millisecond)
	started := time.Now()
	close(gate)
	deadline := time.NewTimer(cfg.timeout)
	ticker := time.NewTicker(10 * time.Millisecond)
	for processed.Load() < int64(cfg.messages) {
		select {
		case <-deadline.C:
			return r, fmt.Errorf("timeout after processing %d/%d", processed.Load(), cfg.messages)
		case <-ticker.C:
		}
	}
	engineElapsed := time.Since(started)
	r.Processed = processed.Load()
	r.ProcessErrors = processErrors.Load()
	r.OutputAccepted, r.OutputErrors = output.counts()
	r.EngineDurationMS = engineElapsed.Milliseconds()
	r.EnginePerSecond = float64(r.Processed) / engineElapsed.Seconds()
	r.MaxInflight = maxInflight.Load()
	r.ProcessP50US, r.ProcessP95US, r.ProcessP99US = percentiles(latencies.values)
	if !deadline.Stop() {
		select {
		case <-deadline.C:
		default:
		}
	}
	ticker.Stop()
	cancel()
	drainCtx, drainCancel := context.WithTimeout(context.Background(), 15*time.Second)
	if err := weighted.Drain(drainCtx); err != nil {
		drainCancel()
		return r, fmt.Errorf("drain input: %w", err)
	}
	drainCancel()
	flushCtx, flushCancel := context.WithTimeout(context.Background(), 30*time.Second)
	if err := output.Flush(flushCtx); err != nil {
		flushCancel()
		r.DurationMS = time.Since(started).Milliseconds()
		return r, fmt.Errorf("flush output: %w", err)
	}
	flushCancel()
	elapsed := time.Since(started)

	_, r.OutputErrors = output.counts()
	r.DurationMS = elapsed.Milliseconds()
	r.MessagesPerSecond = float64(r.Processed) / elapsed.Seconds()
	if r.ProcessErrors != 0 || r.OutputErrors != 0 || r.Processed != int64(cfg.messages) || r.OutputAccepted != int64(cfg.messages) {
		return r, errors.New("message, processing, or output count mismatch")
	}
	return r, nil
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
