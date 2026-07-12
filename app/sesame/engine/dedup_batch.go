package engine

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/valkey-io/valkey-go"
)

const (
	defaultDedupBatchSize = 128
	defaultDedupBatchWait = 200 * time.Microsecond
	dedupBatchQueueSize   = 16_384
)

type dedupClaimResult struct {
	claimed bool
	err     error
}

type dedupClaimRequest struct {
	key    string
	result chan dedupClaimResult
}

// BatchedValkeyDedup preserves Valkey's cross-pod SET NX correctness while
// coalescing concurrent claims into one pipelined round trip. A firehose cohort
// pays one network wait per batch instead of one wait per event.
type BatchedValkeyDedup struct {
	client    valkey.Client
	ttl       time.Duration
	batchSize int
	batchWait time.Duration
	requests  chan dedupClaimRequest
	stop      chan struct{}
	done      chan struct{}

	mu     sync.RWMutex
	closed bool
}

func NewBatchedValkeyDedup(client valkey.Client, ttl time.Duration, batchSize int, batchWait time.Duration) *BatchedValkeyDedup {
	if ttl <= 0 {
		ttl = 10 * time.Minute
	}
	if batchSize <= 0 {
		batchSize = defaultDedupBatchSize
	}
	if batchWait <= 0 {
		batchWait = defaultDedupBatchWait
	}
	d := &BatchedValkeyDedup{
		client: client, ttl: ttl, batchSize: batchSize, batchWait: batchWait,
		requests: make(chan dedupClaimRequest, dedupBatchQueueSize),
		stop:     make(chan struct{}), done: make(chan struct{}),
	}
	go d.run()
	return d
}

func (d *BatchedValkeyDedup) Claim(ctx context.Context, key string) (bool, error) {
	request := dedupClaimRequest{key: key, result: make(chan dedupClaimResult, 1)}
	d.mu.RLock()
	if d.closed {
		d.mu.RUnlock()
		return false, errors.New("sesame: dedup batcher is closed")
	}
	select {
	case d.requests <- request:
		d.mu.RUnlock()
	case <-ctx.Done():
		d.mu.RUnlock()
		return false, ctx.Err()
	}
	select {
	case result := <-request.result:
		return result.claimed, result.err
	case <-ctx.Done():
		return false, ctx.Err()
	}
}

func (d *BatchedValkeyDedup) Release(ctx context.Context, key string) error {
	return d.client.Do(ctx, d.client.B().Del().Key(key).Build()).Error()
}

func (d *BatchedValkeyDedup) Close() {
	d.mu.Lock()
	if d.closed {
		d.mu.Unlock()
		return
	}
	d.closed = true
	close(d.stop)
	d.mu.Unlock()
	<-d.done
}

func (d *BatchedValkeyDedup) run() {
	defer close(d.done)
	for {
		select {
		case first := <-d.requests:
			d.process(d.collect(first))
		case <-d.stop:
			d.drain()
			return
		}
	}
}

func (d *BatchedValkeyDedup) collect(first dedupClaimRequest) []dedupClaimRequest {
	batch := make([]dedupClaimRequest, 1, d.batchSize)
	batch[0] = first
	timer := time.NewTimer(d.batchWait)
	defer timer.Stop()
	for len(batch) < d.batchSize {
		select {
		case request := <-d.requests:
			batch = append(batch, request)
		case <-timer.C:
			return batch
		case <-d.stop:
			return batch
		}
	}
	return batch
}

func (d *BatchedValkeyDedup) drain() {
	for {
		select {
		case first := <-d.requests:
			d.process(d.collectAvailable(first))
		default:
			return
		}
	}
}

func (d *BatchedValkeyDedup) collectAvailable(first dedupClaimRequest) []dedupClaimRequest {
	batch := make([]dedupClaimRequest, 1, d.batchSize)
	batch[0] = first
	for len(batch) < d.batchSize {
		select {
		case request := <-d.requests:
			batch = append(batch, request)
		default:
			return batch
		}
	}
	return batch
}

func (d *BatchedValkeyDedup) process(batch []dedupClaimRequest) {
	commands := make([]valkey.Completed, len(batch))
	for i := range batch {
		commands[i] = d.client.B().Set().Key(batch[i].key).Value("1").Nx().PxMilliseconds(d.ttl.Milliseconds()).Build()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	results := d.client.DoMulti(ctx, commands...)
	cancel()
	for i, result := range results {
		value, err := result.ToString()
		if valkey.IsValkeyNil(err) {
			batch[i].result <- dedupClaimResult{claimed: false}
			continue
		}
		batch[i].result <- dedupClaimResult{claimed: value == "OK", err: err}
	}
}
