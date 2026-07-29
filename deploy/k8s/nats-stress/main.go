// Command nats-stress finds the ceiling and the stable rate of the rewritten
// NATS bus, and validates the idempotency guard at that rate.
//
// It is deliberately made of production parts. The publisher role is
// pkg/bus.NewPublisherForStream driving the NATS 2.14 Fast-Ingest wire; the
// consumer role is pkg/bus's AckFlowControl flow consumer behind
// pkg/bus.ConsumeWeighted; the guard is pkg/idempotency wired exactly as sesame
// wires it (a bounded per-pod LRU in front of a Valkey store pinned to the
// Sentinel-elected primary). Nothing in this directory reimplements a transport,
// so a number it reports is a number the services will see.
//
// It runs against a disposable R3 memory stream on its own subject tree, so no
// production lane is touched — but it runs against the LIVE broker and the LIVE
// Valkey primary, which is why run.sh refuses to start without an explicit
// confirmation.
//
// Roles:
//
//	provision  converge the shadow stream and exit (needs the admin credential)
//	teardown   delete the shadow stream and exit
//	publish    ramp offered load until the ceiling, then soak below it
//	consume    bind the lane, account deliveries, validate the guard
//	report     merge captured summaries into one verdict (no cluster needed)
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"ItsBagelBot/pkg/env"

	"go.uber.org/zap"
)

const (
	roleProvision = "provision"
	roleTeardown  = "teardown"
	rolePublish   = "publish"
	roleConsume   = "consume"
	roleReport    = "report"
)

type options struct {
	role      string
	url       string
	subject   string
	group     string
	identity  string
	runFor    time.Duration
	holdOpen  bool
	tickEvery time.Duration

	publisher publisherConfig
	consumer  consumerConfig
	merge     mergeConfig
	reportIn  []string
}

func main() {
	opts, err := parseOptions(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	log := newLogger()
	defer func() { _ = log.Sync() }()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := dispatch(ctx, opts, log); err != nil {
		log.Error("nats-stress failed", zap.String("role", opts.role), zap.Error(err))
		os.Exit(1)
	}
}

// newLogger sends every log line to stderr, because stdout carries the run's
// machine-readable records and a single interleaved zap line would make the
// whole capture unparseable. It also installs the global, which is not
// cosmetic: pkg/bus reports NATS permission violations and slow-consumer drops
// through zap.L() and nowhere else, and those are exactly the failures a bench
// against a live ACL hits first.
func newLogger() *zap.Logger {
	cfg := zap.NewProductionConfig()
	cfg.OutputPaths = []string{"stderr"}
	cfg.ErrorOutputPaths = []string{"stderr"}
	log, err := cfg.Build()
	if err != nil {
		log = zap.NewNop()
	}
	zap.ReplaceGlobals(log)
	return log
}

func dispatch(ctx context.Context, opts options, log *zap.Logger) error {
	switch opts.role {
	case roleProvision:
		return provisionStream(ctx, opts.url, log)
	case roleTeardown:
		return teardownStream(ctx)
	case rolePublish:
		return runBoundedPublisher(ctx, opts, log)
	case roleConsume:
		return runBoundedConsumer(ctx, opts, log)
	case roleReport:
		return runReport(opts.reportIn, opts.merge)
	default:
		return fmt.Errorf("nats-stress: unknown role %q", opts.role)
	}
}

// runBoundedPublisher applies the run's own deadline and then, by default, holds
// the process open. Holding open matters: a Deployment restarts a container that
// exits, and a publisher that exits at the end of its soak would silently start
// a second full ramp against production.
func runBoundedPublisher(ctx context.Context, opts options, log *zap.Logger) error {
	runCtx, cancel := runDeadline(ctx, opts.runFor)
	defer cancel()
	if err := runPublisher(runCtx, opts.publisher, log); err != nil {
		return err
	}
	return holdOpen(ctx, opts, log)
}

// runBoundedConsumer provisions the stream before binding. The consumer role
// carries the only credential the ACL grants STREAM.CREATE/UPDATE on this name,
// so it is the only role that CAN provision — which is also why run.sh starts
// the consumer first and waits for it before the publishers exist.
func runBoundedConsumer(ctx context.Context, opts options, log *zap.Logger) error {
	if err := provisionStream(ctx, opts.url, log); err != nil {
		return err
	}
	emit(readyLine{
		envelopeHeader: envelopeHeader{Rig: rigName, Role: roleConsume, Kind: "stream_ready"},
		Stream:         stressStreamName, Subject: opts.subject,
	})
	runCtx, cancel := runDeadline(ctx, opts.runFor)
	defer cancel()
	if err := runConsumer(runCtx, opts.consumer, log); err != nil {
		return err
	}
	return holdOpen(ctx, opts, log)
}

type readyLine struct {
	envelopeHeader
	Stream  string `json:"stream"`
	Subject string `json:"subject"`
}

func runDeadline(ctx context.Context, runFor time.Duration) (context.Context, context.CancelFunc) {
	if runFor <= 0 {
		return context.WithCancel(ctx)
	}
	return context.WithTimeout(ctx, runFor)
}

func holdOpen(ctx context.Context, opts options, log *zap.Logger) error {
	if !opts.holdOpen {
		return nil
	}
	log.Info("run complete; holding open until terminated")
	<-ctx.Done()
	return nil
}

// publishWire and publishConnections report the publisher shape the run actually
// used, read from the same variables pkg/bus reads. Recorded in the summary
// because a ceiling is meaningless without the wire it was measured on.
func publishWire() string     { return env.Get("NATS_PUBLISH_WIRE", "fast") }
func publishConnections() int { return env.GetInt("NATS_PUBLISH_CONNECTIONS", 4) }
func podIdentity(prefix string) string {
	if name := env.Get("POD_NAME", env.Get("HOSTNAME", "")); name != "" {
		return name
	}
	return prefix + "-local"
}

func parseOptions(args []string) (options, error) {
	var o options
	fs := flag.NewFlagSet("nats-stress", flag.ContinueOnError)
	bindCommon(fs, &o)
	bindPublisher(fs, &o)
	bindConsumer(fs, &o)
	bindMerge(fs, &o)
	if err := fs.Parse(args); err != nil {
		return o, err
	}
	if !validConsumeMode(o.consumer.Mode) {
		return o, fmt.Errorf("nats-stress: unknown -consume-mode %q (want %s or %s)",
			o.consumer.Mode, consumeModeFlow, consumeModePull)
	}
	o.reportIn = fs.Args()
	return o.resolved(), nil
}

func bindCommon(fs *flag.FlagSet, o *options) {
	fs.StringVar(&o.role, "role", rolePublish, "provision|teardown|publish|consume|report")
	// Empty by default: pkg/bus resolves NATS_HUB_URL / NATS_HUB_PUBLISH_URL on
	// its own, and a flag default here would quietly override the endpoint split
	// the production env block sets up.
	fs.StringVar(&o.url, "url", env.Get("NATS_URL", ""), "fallback NATS endpoint; NATS_HUB_URL still wins")
	fs.StringVar(&o.subject, "subject", stressHotSubject, "hot lane subject")
	fs.StringVar(&o.group, "group", "nats-stress", "consumer durable group")
	fs.StringVar(&o.identity, "id", "", "publisher/consumer identity (defaults to POD_NAME)")
	fs.DurationVar(&o.runFor, "for", 0, "hard cap on the run (0 = until the role finishes or is signalled)")
	fs.BoolVar(&o.holdOpen, "hold-open", true, "stay alive after the run so a Deployment does not restart it")
	fs.DurationVar(&o.tickEvery, "tick", 5*time.Second, "periodic JSON line interval")
}

func bindPublisher(fs *flag.FlagSet, o *options) {
	p := &o.publisher
	fs.IntVar(&p.Lanes, "lanes", 8, "ordered publisher lanes (goroutines)")
	fs.IntVar(&p.Replicas, "replicas", env.GetInt("STRESS_PUBLISHER_REPLICAS", 1),
		"how many publisher replicas share the offered rate")
	fs.IntVar(&p.PayloadBytes, "payload-bytes", 512, "approximate marshalled envelope size")
	fs.Float64Var(&p.DupFraction, "dup-fraction", 0.01,
		"fraction of events published twice under one id; an equal share becomes the control set")
	fs.DurationVar(&p.DupDelay, "dup-delay", 250*time.Millisecond, "delay for the delayed duplicate variant")
	fs.IntVar(&p.SampleEvery, "latency-sample-every", 512, "publish every Nth message confirmed, to time it")
	fs.IntVar(&p.SampleCap, "latency-reservoir", 20000, "latency samples retained per step")
	fs.Float64Var(&p.Plan.Start, "start-rate", 20000, "fleet-wide offered msg/s at step 0")
	fs.Float64Var(&p.Plan.Step, "step-rate", 10000, "fleet-wide msg/s added per step")
	fs.DurationVar(&p.Plan.Hold, "step-hold", 30*time.Second, "how long each step is offered")
	fs.IntVar(&p.Plan.MaxSteps, "max-steps", 20, "ramp ceiling in steps")
	fs.Float64Var(&p.Plan.Overrun, "step-overrun", 2, "multiple of step-hold a step may take before it is cut short")
	fs.Float64Var(&p.StableFrac, "stable-fraction", 0.85, "fraction of the ceiling the soak runs at")
	fs.DurationVar(&p.SoakFor, "soak", 5*time.Minute, "soak duration at the stable rate")
}

func bindConsumer(fs *flag.FlagSet, o *options) {
	c := &o.consumer
	// The A/B knob. CONSUME_MODE is the env consumer.yaml carries, so run.sh sets
	// the flag and the pod's environment from the same variable and they cannot
	// disagree about which contract the run measured.
	fs.StringVar(&c.Mode, "consume-mode", env.Get("CONSUME_MODE", consumeModeFlow),
		"lane acknowledgement contract under test: flow (per-pod push) | pull (shared durable)")
	fs.IntVar(&c.Routines, "routines", 100, "handler routines (pinned; production sesame runs 100)")
	fs.BoolVar(&c.GuardEnabled, "guard", true, "run guard-sampled events through pkg/idempotency")
	fs.StringVar(&c.GuardPrefix, "guard-prefix", "", "Valkey claim prefix (default stress:seen:<pod>:)")
	fs.DurationVar(&c.GuardTTL, "guard-ttl", 2*time.Minute, "idempotency claim ttl")
	fs.IntVar(&c.GuardLRU, "guard-lru", 100000, "per-pod LRU claim cache capacity (sesame runs 100k)")
	fs.DurationVar(&c.LedgerWindow, "guard-window", time.Minute, "how long an event id stays classifiable")
	fs.DurationVar(&c.LagEvery, "lag-interval", time.Second, "receipt-lag sampling interval")
	fs.StringVar(&c.ValkeyAddr, "valkey", env.Get("VALKEY_ADDR", "127.0.0.1:6379"), "Valkey endpoint")
}

func bindMerge(fs *flag.FlagSet, o *options) {
	m := &o.merge
	fs.Float64Var(&m.Detector.Ratio, "ceiling-ratio", 0.97,
		"fraction of the offered rate a step must sustain")
	fs.Float64Var(&m.Detector.MaxFailureRate, "max-failure-rate", 0,
		"failure rate above which a step breaches regardless of throughput")
	fs.Float64Var(&m.LagTolerance, "lag-tolerance", 1.5, "allowed growth between the halves of the lag curve")
	fs.Float64Var(&m.LagFloor, "lag-floor", 5000, "pending count below which lag growth is noise")
	fs.Float64Var(&m.LatencyTol, "latency-tolerance", 1.5, "allowed soak p99 growth over the ramp baseline")
}

// resolved fills in everything that depends on another flag or on the pod's own
// environment, so the roles never have to re-derive it.
func (o options) resolved() options {
	o.merge.StableFrac = o.publisher.StableFrac
	o.publisher.URL, o.publisher.Subject = o.url, o.subject
	o.publisher.PublisherID = firstNonEmpty(o.identity, podIdentity("publisher"))
	o.publisher.Plan = o.publisher.Plan.normalized()
	o.publisher.Detector = o.merge.Detector.normalized()
	o.publisher.TickEvery = o.tickEvery
	o.publisher.LatencyTol = o.merge.LatencyTol

	o.consumer.URL, o.consumer.Subject, o.consumer.Group = o.url, o.subject, o.group
	o.consumer.ConsumerID = firstNonEmpty(o.identity, podIdentity("consumer"))
	o.consumer.TickEvery = o.tickEvery
	o.consumer.SampleCap = o.publisher.SampleCap
	o.consumer.ValkeyPass = env.Get("VALKEY_PASSWORD", "")
	o.consumer.GuardPrefix = guardPrefix(o.consumer.GuardPrefix, o.consumer.ConsumerID)
	return o
}

// guardPrefix defaults to a per-pod namespace under a rig-owned root.
//
// Two separate reasons, both load-bearing. The root must never be sesame's
// "sesame:seen:", or a bench claim and a live claim can collide on the same
// primary and the rig silently suppresses a production event. The per-pod suffix
// exists because every consumer pod owns its own flow consumer and therefore
// receives the WHOLE lane: with a shared namespace the second pod's claims are
// all won by the first, which is correct production behaviour but makes an
// injected duplicate indistinguishable from a cross-pod claim in the per-pod
// ledger. Pass -guard-prefix stress:seen: explicitly to measure the shared-claim
// shape instead; the Valkey load is identical either way.
func guardPrefix(configured, identity string) string {
	if configured != "" {
		return configured
	}
	return "stress:seen:" + identity + ":"
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
