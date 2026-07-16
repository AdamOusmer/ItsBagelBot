package main

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"ItsBagelBot/pkg/bus"
	sharedvalkey "ItsBagelBot/pkg/valkey"
	"github.com/nats-io/nats.go"
	valkey_go "github.com/valkey-io/valkey-go"
)

const (
	sliRequestPayload              = `{}`
	valkeyConvergenceRetryInterval = time.Millisecond
)

type sliRPCSample struct {
	Service string  `json:"service"`
	RTTMS   float64 `json:"rtt_ms"`
}

type sliIngressSample struct {
	Subject             string  `json:"subject"`
	RTTMS               float64 `json:"rtt_ms"`
	ConduitManagerState string  `json:"conduit_manager_state"`
	DesiredCount        int     `json:"desired_count"`
	ShardCount          int     `json:"shard_count"`
}

type sliValkeySample struct {
	Key      string  `json:"key"`
	PingRTT  float64 `json:"ping_rtt_ms"`
	SetRTT   float64 `json:"set_rtt_ms"`
	GetRTT   float64 `json:"get_rtt_ms"`
	KeyTTLMS int64   `json:"key_ttl_ms"`
}

type sliSample struct {
	Type       string            `json:"type"`
	SampledAt  time.Time         `json:"sampled_at"`
	Sequence   int64             `json:"sequence"`
	DurationMS float64           `json:"duration_ms"`
	MaxRTTMS   float64           `json:"max_rtt_ms"`
	RPC        []sliRPCSample    `json:"rpc"`
	Ingress    *sliIngressSample `json:"ingress,omitempty"`
	Valkey     *sliValkeySample  `json:"valkey,omitempty"`
	Passed     bool              `json:"passed"`
	Failure    string            `json:"failure,omitempty"`
}

type sliProbes struct {
	request  func(context.Context, string) ([]byte, error)
	ping     func(context.Context) (string, error)
	set      func(context.Context, string, string, time.Duration) (string, error)
	get      func(context.Context, string) (string, error)
	healthy  func() error
	failures <-chan error
	close    func()
}

type ingressAttemptTracker struct {
	previousAttempts map[int]int
	previousSessions map[int]string
	previousBoundAt  map[int]time.Time
	previousDesired  int
}

type ingressShardSnapshot struct {
	ConduitManager struct {
		State string `json:"state"`
	} `json:"conduit_manager"`
	DesiredCount int            `json:"desired_count"`
	Shards       []ingressShard `json:"shards"`
}

type ingressShard struct {
	ShardID           *int       `json:"shard_id"`
	State             string     `json:"state"`
	SessionID         string     `json:"session_id"`
	Bound             *bool      `json:"bound"`
	BoundAt           *time.Time `json:"bound_at"`
	HandshakeInFlight *bool      `json:"handshake_in_flight"`
	KeepaliveMS       *int64     `json:"keepalive_ms"`
	Attempts          *int       `json:"attempts"`
	LastFrameAt       *time.Time `json:"last_frame_at"`
}

func (r *acceptanceRun) executeSLI() error {
	probes, err := newSLIProbes(r.cfg, r.hub, r.tlsConfig)
	if err != nil {
		return err
	}
	defer probes.close()
	return runContinuousSLI(context.Background(), r.cfg, probes, os.Stdout)
}

func newSLIProbes(cfg config, ep endpoint, tlsConfig *tls.Config) (*sliProbes, error) {
	stats := &connectionStats{failures: make(chan error, 1)}
	nc, err := connectCore(cfg, ep, tlsConfig, "live-acceptance-sli", stats)
	if err != nil {
		return nil, fmt.Errorf("connect persistent NATS RPC SLI client: %w", err)
	}
	vc, err := sharedvalkey.NewClient(cfg.valkeyAddress, cfg.valkeyPassword)
	if err != nil {
		nc.Close()
		return nil, fmt.Errorf("connect persistent Valkey SLI client: %w", err)
	}
	return &sliProbes{
		request: func(ctx context.Context, subject string) ([]byte, error) {
			msg, err := nc.RequestWithContext(ctx, subject, []byte(sliRequestPayload))
			if err != nil {
				return nil, err
			}
			return msg.Data, nil
		},
		ping: func(ctx context.Context) (string, error) {
			return vc.Do(ctx, vc.B().Ping().Build()).ToString()
		},
		set: func(ctx context.Context, key, value string, ttl time.Duration) (string, error) {
			cmd := vc.B().Set().Key(key).Value(value).PxMilliseconds(ttl.Milliseconds()).Build()
			return vc.Do(ctx, cmd).ToString()
		},
		get: func(ctx context.Context, key string) (string, error) {
			return vc.Do(ctx, vc.B().Get().Key(key).Build()).ToString()
		},
		healthy: func() error {
			return validateSLIConnection(nc, stats)
		},
		failures: stats.failures,
		close: func() {
			vc.Close()
			nc.Close()
		},
	}, nil
}

func validateSLIConnection(nc *nats.Conn, stats *connectionStats) error {
	if reconnects := stats.reconnects.Load(); reconnects != 0 {
		return fmt.Errorf("NATS SLI connection reconnected %d time(s)", reconnects)
	}
	if disconnects := stats.disconnects.Load(); disconnects != 0 {
		return fmt.Errorf("NATS SLI connection disconnected %d time(s)", disconnects)
	}
	if asyncErrors := stats.asyncErrors.Load(); asyncErrors != 0 {
		return fmt.Errorf("NATS SLI connection observed %d asynchronous error(s)", asyncErrors)
	}
	if !nc.IsConnected() {
		return errors.New("NATS SLI connection is not connected")
	}
	return nil
}

func runContinuousSLI(ctx context.Context, cfg config, probes *sliProbes, output io.Writer) error {
	runCtx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	if probes.failures != nil {
		go func() {
			select {
			case err := <-probes.failures:
				if err != nil {
					cancel(err)
				}
			case <-runCtx.Done():
			}
		}()
	}

	encoder := json.NewEncoder(output)
	duration := time.NewTimer(cfg.sliDuration)
	defer duration.Stop()
	tracker := &ingressAttemptTracker{}
	for sequence := int64(1); ; sequence++ {
		sample, err := collectSLISample(runCtx, cfg, probes, tracker, sequence)
		if cause := context.Cause(runCtx); cause != nil {
			err = cause
		}
		sample.Passed = err == nil
		if err != nil {
			sample.Failure = err.Error()
		}
		if encodeErr := encoder.Encode(sample); encodeErr != nil {
			return fmt.Errorf("encode SLI JSONL sample: %w", encodeErr)
		}
		if err != nil {
			return err
		}

		interval := time.NewTimer(cfg.sliInterval)
		select {
		case <-duration.C:
			interval.Stop()
			return probes.healthy()
		case <-runCtx.Done():
			interval.Stop()
			return context.Cause(runCtx)
		case <-interval.C:
		}
	}
}

func collectSLISample(
	ctx context.Context,
	cfg config,
	probes *sliProbes,
	tracker *ingressAttemptTracker,
	sequence int64,
) (sample sliSample, err error) {
	started := time.Now()
	sample = sliSample{
		Type:      "continuous_sli",
		SampledAt: started.UTC(),
		Sequence:  sequence,
		RPC:       make([]sliRPCSample, 0, len(cfg.sliServices)),
	}
	defer func() {
		sample.DurationMS = durationMilliseconds(time.Since(started))
	}()
	if err := probes.healthy(); err != nil {
		return sample, err
	}

	for _, service := range cfg.sliServices {
		data, rtt, err := requestSLI(ctx, cfg, probes, bus.RPCHealthSubject(service))
		sample.MaxRTTMS = max(sample.MaxRTTMS, durationMilliseconds(rtt))
		if err != nil {
			return sample, fmt.Errorf("RPC health %s: %w", service, err)
		}
		if err := validateRPCHealthReply(service, data); err != nil {
			return sample, err
		}
		if err := validateSLIRTT("RPC health "+service, rtt, cfg.sliMaxRTT); err != nil {
			return sample, err
		}
		sample.RPC = append(sample.RPC, sliRPCSample{Service: service, RTTMS: durationMilliseconds(rtt)})
	}

	if cfg.sliIngressSubject != "" {
		data, rtt, err := requestSLI(ctx, cfg, probes, cfg.sliIngressSubject)
		sample.MaxRTTMS = max(sample.MaxRTTMS, durationMilliseconds(rtt))
		if err != nil {
			return sample, fmt.Errorf("ingress shard snapshot: %w", err)
		}
		ingress, err := tracker.validate(data, cfg.sliIngressSubject, rtt)
		if err != nil {
			return sample, fmt.Errorf("ingress shard snapshot: %w", err)
		}
		if err := validateSLIRTT("ingress shard snapshot", rtt, cfg.sliIngressMaxRTT); err != nil {
			return sample, err
		}
		sample.Ingress = &ingress
	}

	keyTTL := sliKeyTTL(cfg.sliInterval)
	valkeySample, maxRTT, err := sampleValkey(ctx, cfg, probes, sequence, keyTTL)
	sample.Valkey = &valkeySample
	sample.MaxRTTMS = max(sample.MaxRTTMS, durationMilliseconds(maxRTT))
	if err != nil {
		return sample, err
	}
	if err := probes.healthy(); err != nil {
		return sample, err
	}
	return sample, nil
}

func requestSLI(ctx context.Context, cfg config, probes *sliProbes, subject string) ([]byte, time.Duration, error) {
	opCtx, cancel := context.WithTimeout(ctx, cfg.sliTimeout)
	defer cancel()
	started := time.Now()
	data, err := probes.request(opCtx, subject)
	rtt := time.Since(started)
	if err != nil {
		return nil, rtt, sliOperationError(ctx, opCtx, cfg.sliTimeout, err)
	}
	return data, rtt, nil
}

func validateRPCHealthReply(service string, data []byte) error {
	var reply bus.RPCHealthReply
	if err := json.Unmarshal(data, &reply); err != nil {
		return fmt.Errorf("RPC health %s returned malformed JSON: %w", service, err)
	}
	if reply.Service != service || !reply.OK {
		return fmt.Errorf("RPC health %s returned invalid reply {service:%q,ok:%t}", service, reply.Service, reply.OK)
	}
	return nil
}

func sampleValkey(
	ctx context.Context,
	cfg config,
	probes *sliProbes,
	sequence int64,
	ttl time.Duration,
) (sliValkeySample, time.Duration, error) {
	sample := sliValkeySample{Key: cfg.sliKey, KeyTTLMS: ttl.Milliseconds()}
	var maximum time.Duration

	pong, rtt, err := timedValkeyString(ctx, cfg, probes.ping)
	sample.PingRTT = durationMilliseconds(rtt)
	maximum = max(maximum, rtt)
	if err != nil {
		return sample, maximum, fmt.Errorf("Valkey PING: %w", err)
	}
	if pong != "PONG" {
		return sample, maximum, fmt.Errorf("Valkey PING returned %q instead of PONG", pong)
	}
	if err := validateSLIRTT("Valkey PING", rtt, cfg.sliMaxRTT); err != nil {
		return sample, maximum, err
	}

	value := fmt.Sprintf("%s:%d:%d", cfg.producerID, sequence, time.Now().UnixNano())
	setReply, rtt, err := timedValkeyString(ctx, cfg, func(opCtx context.Context) (string, error) {
		return probes.set(opCtx, cfg.sliKey, value, ttl)
	})
	sample.SetRTT = durationMilliseconds(rtt)
	maximum = max(maximum, rtt)
	if err != nil {
		return sample, maximum, fmt.Errorf("Valkey SET: %w", err)
	}
	if setReply != "OK" {
		return sample, maximum, fmt.Errorf("Valkey SET returned %q instead of OK", setReply)
	}
	if err := validateSLIRTT("Valkey SET", rtt, cfg.sliMaxRTT); err != nil {
		return sample, maximum, err
	}

	convergenceBudget := min(cfg.sliMaxRTT, cfg.sliTimeout)
	got, rtt, err := waitForValkeyValue(ctx, convergenceBudget, value, func(opCtx context.Context) (string, error) {
		return probes.get(opCtx, cfg.sliKey)
	})
	sample.GetRTT = durationMilliseconds(rtt)
	maximum = max(maximum, rtt)
	if err != nil {
		return sample, maximum, fmt.Errorf("Valkey GET: %w", err)
	}
	if got != value {
		return sample, maximum, fmt.Errorf("Valkey GET value mismatch: got %q", got)
	}
	if err := validateSLIRTT("Valkey GET", rtt, cfg.sliMaxRTT); err != nil {
		return sample, maximum, err
	}
	return sample, maximum, nil
}

func waitForValkeyValue(
	ctx context.Context,
	budget time.Duration,
	expected string,
	get func(context.Context) (string, error),
) (string, time.Duration, error) {
	opCtx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()
	started := time.Now()
	lastValue := ""
	for {
		value, err := get(opCtx)
		elapsed := time.Since(started)
		if err != nil && !valkey_go.IsValkeyNil(err) {
			return value, elapsed, sliOperationError(ctx, opCtx, budget, err)
		}
		if valkey_go.IsValkeyNil(err) {
			value = ""
		}
		lastValue = value
		if value == expected {
			return value, elapsed, nil
		}
		if !waitForValkeyConvergenceRetry(opCtx) {
			if cause := context.Cause(ctx); cause != nil {
				return lastValue, time.Since(started), cause
			}
			return lastValue, time.Since(started), fmt.Errorf(
				"value mismatch after %s convergence budget: got %q",
				budget,
				lastValue,
			)
		}
	}
}

func waitForValkeyConvergenceRetry(ctx context.Context) bool {
	timer := time.NewTimer(valkeyConvergenceRetryInterval)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-ctx.Done():
		return false
	}
}

func timedValkeyString(
	ctx context.Context,
	cfg config,
	operation func(context.Context) (string, error),
) (string, time.Duration, error) {
	opCtx, cancel := context.WithTimeout(ctx, cfg.sliTimeout)
	defer cancel()
	started := time.Now()
	value, err := operation(opCtx)
	rtt := time.Since(started)
	if err != nil {
		return "", rtt, sliOperationError(ctx, opCtx, cfg.sliTimeout, err)
	}
	return value, rtt, nil
}

func sliOperationError(parent, operation context.Context, timeout time.Duration, err error) error {
	if cause := context.Cause(parent); cause != nil {
		return cause
	}
	if errors.Is(operation.Err(), context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("timed out after %s: %w", timeout, err)
	}
	return err
}

func validateSLIRTT(operation string, rtt, maximum time.Duration) error {
	if rtt > maximum {
		return fmt.Errorf("%s RTT %s exceeded max-rtt %s", operation, rtt, maximum)
	}
	return nil
}

func (t *ingressAttemptTracker) validate(data []byte, subject string, rtt time.Duration) (sliIngressSample, error) {
	var snapshot ingressShardSnapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return sliIngressSample{}, fmt.Errorf("malformed JSON: %w", err)
	}
	if snapshot.ConduitManager.State != "running" {
		return sliIngressSample{}, fmt.Errorf("conduit_manager.state=%q, want running", snapshot.ConduitManager.State)
	}
	if snapshot.DesiredCount <= 0 {
		return sliIngressSample{}, fmt.Errorf("desired_count=%d, want > 0", snapshot.DesiredCount)
	}
	if len(snapshot.Shards) != snapshot.DesiredCount {
		return sliIngressSample{}, fmt.Errorf("len(shards)=%d, want desired_count=%d", len(snapshot.Shards), snapshot.DesiredCount)
	}
	if t.previousDesired != 0 && t.previousDesired != snapshot.DesiredCount {
		return sliIngressSample{}, fmt.Errorf(
			"desired_count changed between samples from %d to %d",
			t.previousDesired,
			snapshot.DesiredCount,
		)
	}

	byID := make(map[int]ingressShard, len(snapshot.Shards))
	for _, shard := range snapshot.Shards {
		if shard.ShardID == nil {
			return sliIngressSample{}, errors.New("shard is missing shard_id")
		}
		if _, duplicate := byID[*shard.ShardID]; duplicate {
			return sliIngressSample{}, fmt.Errorf("duplicate shard_id=%d", *shard.ShardID)
		}
		if shard.State == "unresponsive" || shard.State == "unregistered" {
			return sliIngressSample{}, fmt.Errorf("shard_id=%d state=%s", *shard.ShardID, shard.State)
		}
		byID[*shard.ShardID] = shard
	}

	nextAttempts := make(map[int]int, snapshot.DesiredCount)
	nextSessions := make(map[int]string, snapshot.DesiredCount)
	nextBoundAt := make(map[int]time.Time, snapshot.DesiredCount)
	sameDesiredCount := t.previousDesired == snapshot.DesiredCount
	for shardID := 0; shardID < snapshot.DesiredCount; shardID++ {
		shard, exists := byID[shardID]
		if !exists {
			return sliIngressSample{}, fmt.Errorf("desired shard_id=%d is missing", shardID)
		}
		if shard.State != "connected" {
			return sliIngressSample{}, fmt.Errorf("desired shard_id=%d state=%q, want connected", shardID, shard.State)
		}
		if shard.Bound == nil || !*shard.Bound {
			return sliIngressSample{}, fmt.Errorf("desired shard_id=%d is not bound", shardID)
		}
		if shard.SessionID == "" {
			return sliIngressSample{}, fmt.Errorf("desired shard_id=%d is missing session_id", shardID)
		}
		if shard.BoundAt == nil || shard.BoundAt.IsZero() {
			return sliIngressSample{}, fmt.Errorf("desired shard_id=%d is missing bound_at", shardID)
		}
		if previous, exists := t.previousSessions[shardID]; sameDesiredCount && exists && previous != shard.SessionID {
			return sliIngressSample{}, fmt.Errorf(
				"desired shard_id=%d session_id changed between samples",
				shardID,
			)
		}
		if previous, exists := t.previousBoundAt[shardID]; sameDesiredCount && exists && !previous.Equal(*shard.BoundAt) {
			return sliIngressSample{}, fmt.Errorf("desired shard_id=%d bound_at changed between samples", shardID)
		}
		if shard.HandshakeInFlight == nil || *shard.HandshakeInFlight {
			return sliIngressSample{}, fmt.Errorf("desired shard_id=%d has a handshake in flight", shardID)
		}
		if shard.KeepaliveMS == nil || *shard.KeepaliveMS <= 0 {
			return sliIngressSample{}, fmt.Errorf("desired shard_id=%d has no valid keepalive_ms", shardID)
		}
		if shard.LastFrameAt == nil {
			return sliIngressSample{}, fmt.Errorf("desired shard_id=%d is missing last_frame_at", shardID)
		}
		freshnessWindow := time.Duration(*shard.KeepaliveMS)*time.Millisecond + 10*time.Second
		if age := time.Since(*shard.LastFrameAt); age > freshnessWindow || age < -5*time.Second {
			return sliIngressSample{}, fmt.Errorf(
				"desired shard_id=%d last_frame_at age %s is outside freshness window %s",
				shardID,
				age,
				freshnessWindow,
			)
		}
		if shard.Attempts == nil {
			return sliIngressSample{}, fmt.Errorf("desired shard_id=%d is missing attempts", shardID)
		}
		if *shard.Attempts != 0 {
			return sliIngressSample{}, fmt.Errorf("desired shard_id=%d attempts=%d, want zero", shardID, *shard.Attempts)
		}
		if previous, exists := t.previousAttempts[shardID]; sameDesiredCount && exists && previous != *shard.Attempts {
			return sliIngressSample{}, fmt.Errorf("desired shard_id=%d attempts changed from %d to %d", shardID, previous, *shard.Attempts)
		}
		nextAttempts[shardID] = *shard.Attempts
		nextSessions[shardID] = shard.SessionID
		nextBoundAt[shardID] = *shard.BoundAt
	}
	t.previousAttempts = nextAttempts
	t.previousSessions = nextSessions
	t.previousBoundAt = nextBoundAt
	t.previousDesired = snapshot.DesiredCount
	return sliIngressSample{
		Subject:             subject,
		RTTMS:               durationMilliseconds(rtt),
		ConduitManagerState: snapshot.ConduitManager.State,
		DesiredCount:        snapshot.DesiredCount,
		ShardCount:          len(snapshot.Shards),
	}, nil
}

func sliKeyTTL(interval time.Duration) time.Duration {
	return max(30*time.Second, 3*interval)
}

func durationMilliseconds(duration time.Duration) float64 {
	return float64(duration.Nanoseconds()) / float64(time.Millisecond)
}
