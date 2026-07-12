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
	runID := strings.ToLower(time.Now().UTC().Format("20060102t150405"))
	cfg := config{}
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
	flag.Parse()
	if cfg.outputURL == "" {
		cfg.outputURL = cfg.url
	}
	if cfg.outputMode != "nats" && cfg.outputMode != "memory" {
		panic("output must be nats or memory")
	}
	if cfg.messages < 1 || cfg.channels < 1 || cfg.minRoutines < 1 || cfg.maxRoutines < cfg.minRoutines || cfg.minConsumers < 1 || cfg.maxConsumers < cfg.minConsumers {
		panic("invalid message or routine limits")
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
	if cfg.user == "" || cfg.password == "" || cfg.subUser == "" || cfg.subPassword == "" {
		panic("publisher and subscriber NATS credentials are required")
	}
	return cfg
}

func run(cfg config) (r result, returnErr error) {
	r = result{
		Node: os.Getenv("NODE_NAME"), Messages: cfg.messages, Channels: cfg.channels, GOMAXPROCS: runtime.GOMAXPROCS(0),
		MinRoutines: cfg.minRoutines, MaxRoutines: cfg.maxRoutines,
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
		fleet, openErr := bus.NewPublisherForStream(cfg.outputURL, cfg.stream, log)
		err = openErr
		if err != nil {
			return r, fmt.Errorf("open output publisher: %w", err)
		}
		output = &measuredFleetPublisher{Publisher: fleet}
	}
	defer output.Close() //nolint:errcheck -- explicit flush below reports failures

	pipe := engine.NewPipeline(engine.Deps{
		Proj: benchReader{}, Live: liveAlways{}, Cooldown: engine.NoopCooldown{},
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
	var processed, uniqueProcessed, processErrors, inflight, maxInflight atomic.Int64
	var seen sync.Map
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
		if _, loaded := seen.LoadOrStore(msg.UUID, struct{}{}); !loaded {
			uniqueProcessed.Add(1)
		}
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
	for uniqueProcessed.Load() < int64(cfg.messages) {
		select {
		case <-deadline.C:
			return r, fmt.Errorf("timeout after processing %d/%d unique inputs (%d attempts)", uniqueProcessed.Load(), cfg.messages, processed.Load())
		case <-ticker.C:
		}
	}
	engineElapsed := time.Since(started)
	r.Processed = processed.Load()
	r.UniqueProcessed = uniqueProcessed.Load()
	r.ProcessErrors = processErrors.Load()
	r.OutputAccepted, r.OutputErrors, r.OutputDuplicates = output.counts()
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

	_, r.OutputErrors, r.OutputDuplicates = output.counts()
	r.DurationMS = elapsed.Milliseconds()
	stateDeadline := time.Now().Add(5 * time.Second)
	for {
		info, infoErr := js.StreamInfo(cfg.stream, &nats.StreamInfoRequest{SubjectsFilter: ">"})
		if infoErr != nil {
			return r, fmt.Errorf("inspect unique outputs: %w", infoErr)
		}
		r.OutputStored = info.State.Subjects[cfg.output]
		r.InputStored = info.State.Subjects[cfg.input]
		r.StreamStored = info.State.Msgs
		if r.OutputStored >= uint64(cfg.messages) || time.Now().After(stateDeadline) {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	r.MessagesPerSecond = float64(cfg.messages) / elapsed.Seconds()
	storedMismatch := cfg.outputMode == "nats" && r.OutputStored != uint64(cfg.messages)
	if r.ProcessErrors != 0 || r.OutputErrors != 0 || r.UniqueProcessed != int64(cfg.messages) || r.OutputAccepted < int64(cfg.messages) || storedMismatch {
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
