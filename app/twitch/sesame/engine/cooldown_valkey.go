// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"strconv"
	"time"

	"github.com/valkey-io/valkey-go"
)

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
			return false, nil
		}
		return false, err
	}
	return str == "OK", nil
}

var allowAllScript = valkey.NewLuaScript(`
if redis.call('EXISTS', unpack(KEYS)) > 0 then return 0 end
for i, key in ipairs(KEYS) do
  redis.call('SET', key, '1', 'PX', ARGV[i])
end
return 1`)

func (c *ValkeyCooldown) AllowAll(ctx context.Context, windows []CooldownWindow) (bool, error) {
	keys := make([]string, len(windows))
	ttls := make([]string, len(windows))
	for i, w := range windows {
		keys[i] = w.Key
		ttls[i] = strconv.FormatInt(w.TTL.Milliseconds(), 10)
	}
	claimed, err := allowAllScript.Exec(ctx, c.client, keys, ttls).AsInt64()
	return claimed == 1, err
}
