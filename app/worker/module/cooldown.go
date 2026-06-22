package module

import (
	"context"
	"time"

	"github.com/valkey-io/valkey-go"
)

// CooldownStore gates an action behind a shared cooldown window. Allow returns
// true when the caller may proceed (the window was free and is now claimed) and
// false while a previous claim is still cooling down.
type CooldownStore interface {
	Allow(ctx context.Context, key string, ttl time.Duration) (bool, error)
}

// ValkeyCooldown claims a cooldown with SET key 1 NX PX ttl: the first caller in
// the window wins the key, everyone else sees it already set. It is one round
// trip and correct across replicas, the same idiom outgress uses for its enroll
// lock.
type ValkeyCooldown struct {
	client valkey.Client
}

func NewValkeyCooldown(client valkey.Client) *ValkeyCooldown {
	return &ValkeyCooldown{client: client}
}

func (c *ValkeyCooldown) Allow(ctx context.Context, key string, ttl time.Duration) (bool, error) {
	res := c.client.Do(ctx, c.client.B().Set().Key(key).Value("1").Nx().PxMilliseconds(ttl.Milliseconds()).Build())
	str, err := res.ToString()
	if err != nil {
		if valkey.IsValkeyNil(err) {
			return false, nil // key already present: still cooling down
		}
		return false, err
	}
	return str == "OK", nil
}

// NoopCooldown never gates: every call is allowed. Used in tests and when no
// cooldown backend is configured.
type NoopCooldown struct{}

func (NoopCooldown) Allow(context.Context, string, time.Duration) (bool, error) { return true, nil }
