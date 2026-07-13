// Command valkey-live-acceptance measures the shared Go client's production
// read and write routes from one Kubernetes node. It uses an isolated TTL key
// and reports client-observed percentiles, including network and queueing time.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	shared "ItsBagelBot/pkg/valkey"
	valkey "github.com/valkey-io/valkey-go"
)

type config struct {
	address       string
	caFile        string
	mode          string
	node          string
	concurrency   int
	requests      int
	warmup        int
	timeout       time.Duration
	target        time.Duration
	requireTarget bool
}

type operation struct {
	name string
	key  string
}

type result struct {
	Node               string  `json:"node"`
	Operation          string  `json:"operation"`
	Concurrency        int     `json:"concurrency"`
	Requests           int     `json:"requests"`
	Throughput         float64 `json:"throughput_per_second"`
	P50Microseconds    float64 `json:"p50_us"`
	P95Microseconds    float64 `json:"p95_us"`
	P99Microseconds    float64 `json:"p99_us"`
	MaxMicroseconds    float64 `json:"max_us"`
	Errors             int64   `json:"errors"`
	TargetMicroseconds float64 `json:"target_us"`
	Passed             bool    `json:"passed"`
}

type measurement struct {
	latencies []time.Duration
	elapsed   time.Duration
	errors    int64
}

func main() {
	cfg := parseFlags()
	if err := installCA(cfg.caFile); err != nil {
		fatal(err)
	}

	client, err := shared.NewClient(cfg.address, os.Getenv("REDISCLI_AUTH"))
	if err != nil {
		fatal(err)
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), cfg.timeout)
	defer cancel()
	key := fmt.Sprintf("acceptance:valkey:p99:%s:%d", cfg.node, time.Now().UnixNano())
	if err := seed(ctx, client, key); err != nil {
		fatal(err)
	}
	defer client.Do(context.Background(), client.B().Del().Key(key).Build())

	failed := false
	for _, op := range cfg.operations(key) {
		run(ctx, client, cfg, op, cfg.warmup)
		measured := run(ctx, client, cfg, op, cfg.requests)
		report := summarize(cfg, op, measured)
		if err := json.NewEncoder(os.Stdout).Encode(report); err != nil {
			fatal(err)
		}
		failed = failed || !report.Passed
	}
	if cfg.requireTarget && failed {
		os.Exit(1)
	}
}

func parseFlags() config {
	var cfg config
	flag.StringVar(&cfg.address, "address", "valkey.valkey.svc.cluster.local:26380", "Sentinel address")
	flag.StringVar(&cfg.caFile, "ca-file", "/etc/valkey/tls/ca.crt", "fleet CA file")
	flag.StringVar(&cfg.mode, "mode", "both", "read, write, or both")
	flag.StringVar(&cfg.node, "node", os.Getenv("NODE_NAME"), "source node label")
	flag.IntVar(&cfg.concurrency, "concurrency", 5, "concurrent callers")
	flag.IntVar(&cfg.requests, "requests", 100000, "requests per measured operation")
	flag.IntVar(&cfg.warmup, "warmup", 5000, "warmup requests per operation")
	flag.DurationVar(&cfg.timeout, "timeout", 5*time.Minute, "whole-run timeout")
	flag.DurationVar(&cfg.target, "target", 2*time.Millisecond, "maximum accepted p99")
	flag.BoolVar(&cfg.requireTarget, "require-target", false, "exit non-zero when any p99 misses target")
	flag.Parse()
	if cfg.node == "" || cfg.concurrency < 1 || cfg.requests < 1 || cfg.warmup < 0 || cfg.target <= 0 {
		fatal(errors.New("invalid benchmark configuration"))
	}
	if cfg.mode != "read" && cfg.mode != "write" && cfg.mode != "both" {
		fatal(errors.New("mode must be read, write, or both"))
	}
	return cfg
}

func installCA(path string) error {
	pem, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read CA: %w", err)
	}
	return os.Setenv("VALKEY_TLS_CA_PEM", string(pem))
}

func (cfg config) operations(key string) []operation {
	if cfg.mode == "read" {
		return []operation{{name: "read-local", key: key}}
	}
	if cfg.mode == "write" {
		return []operation{{name: "write-master", key: key}}
	}
	return []operation{{name: "read-local", key: key}, {name: "write-master", key: key}}
}

func seed(ctx context.Context, client valkey.Client, key string) error {
	cmd := client.B().Set().Key(key).Value("01234567890123456789012345678901").ExSeconds(300).Build()
	if err := client.Do(ctx, cmd).Error(); err != nil {
		return fmt.Errorf("seed key: %w", err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if err := client.Do(ctx, client.B().Get().Key(key).Build()).Error(); err == nil {
			return nil
		}
		time.Sleep(10 * time.Millisecond)
	}
	return errors.New("seed did not reach node-local replica")
}

func run(ctx context.Context, client valkey.Client, cfg config, op operation, requests int) measurement {
	latencies := make([]time.Duration, requests)
	var next atomic.Int64
	var failures atomic.Int64
	start := make(chan struct{})
	var workers sync.WaitGroup
	workers.Add(cfg.concurrency)
	started := time.Now()
	for range cfg.concurrency {
		go func() {
			defer workers.Done()
			<-start
			for {
				i := int(next.Add(1) - 1)
				if i >= requests {
					return
				}
				began := time.Now()
				if execute(ctx, client, op).Error() != nil {
					failures.Add(1)
				}
				latencies[i] = time.Since(began)
			}
		}()
	}
	close(start)
	workers.Wait()
	return measurement{latencies: latencies, elapsed: time.Since(started), errors: failures.Load()}
}

func execute(ctx context.Context, client valkey.Client, op operation) valkey.ValkeyResult {
	if op.name == "read-local" {
		return client.Do(ctx, client.B().Get().Key(op.key).Build())
	}
	return client.Do(ctx, client.B().Set().Key(op.key).Value("01234567890123456789012345678901").ExSeconds(300).Build())
}

func summarize(cfg config, op operation, measured measurement) result {
	sort.Slice(measured.latencies, func(i, j int) bool { return measured.latencies[i] < measured.latencies[j] })
	p99 := percentile(measured.latencies, 99)
	return result{
		Node: cfg.node, Operation: op.name, Concurrency: cfg.concurrency, Requests: len(measured.latencies),
		Throughput:      float64(len(measured.latencies)) / measured.elapsed.Seconds(),
		P50Microseconds: micros(percentile(measured.latencies, 50)),
		P95Microseconds: micros(percentile(measured.latencies, 95)),
		P99Microseconds: micros(p99), MaxMicroseconds: micros(measured.latencies[len(measured.latencies)-1]),
		Errors: measured.errors, TargetMicroseconds: micros(cfg.target),
		Passed: measured.errors == 0 && p99 <= cfg.target,
	}
}

func percentile(values []time.Duration, percent int) time.Duration {
	index := (len(values)*percent + 99) / 100
	if index < 1 {
		index = 1
	}
	return values[index-1]
}

func micros(value time.Duration) float64 { return float64(value) / float64(time.Microsecond) }

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(2)
}
