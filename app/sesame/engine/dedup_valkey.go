package engine

import (
	"context"
	"time"

	"github.com/valkey-io/valkey-go"
)

// ValkeyDedup folds replicated deliveries by claiming a per-event key with
// SET key 1 NX PX ttl: the first delivery of an EventSub event id wins the key,
// a concurrent duplicate (the same notification re-published by an overlapping
// ingress shard) sees it already set and is dropped. It is one round trip and
// correct across replicas, the same idiom ValkeyCooldown uses for its window.
type ValkeyDedup struct {
	client valkey.Client
	ttl    time.Duration
}

// NewValkeyDedup builds the store over one client. ttl bounds how long a claimed
// EventSub id is remembered; an id never legitimately identifies a second
// notification, so a longer ttl only costs keys and cannot suppress a real event.
func NewValkeyDedup(client valkey.Client, ttl time.Duration) *ValkeyDedup {
	if ttl <= 0 {
		ttl = 10 * time.Minute
	}
	return &ValkeyDedup{client: client, ttl: ttl}
}

// Claim records the first delivery of key and reports whether the caller won it.
// true means this delivery is the first within ttl and should be processed; false
// means a prior delivery already claimed it (a replicated duplicate) and this one
// should be dropped.
func (d *ValkeyDedup) Claim(ctx context.Context, key string) (bool, error) {
	res := d.client.Do(ctx, d.client.B().Set().Key(key).Value("1").Nx().PxMilliseconds(d.ttl.Milliseconds()).Build())
	str, err := res.ToString()
	if err != nil {
		if valkey.IsValkeyNil(err) {
			return false, nil // key already present: replicated duplicate
		}
		return false, err
	}
	return str == "OK", nil
}

// Release drops a claim so a genuine redelivery can retry. It is called only on
// the paths that nack (an infra failure before or during publish); a clean ack
// keeps the claim so replicated duplicates stay folded.
func (d *ValkeyDedup) Release(ctx context.Context, key string) error {
	return d.client.Do(ctx, d.client.B().Del().Key(key).Build()).Error()
}
