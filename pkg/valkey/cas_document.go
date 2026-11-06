// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package valkey

import (
	"context"
	"strconv"
	"time"

	valkey_go "github.com/valkey-io/valkey-go"
)

var casDocumentScript = valkey_go.NewLuaScript(`
local cur = redis.call('GET', KEYS[1])
if (cur == false and ARGV[1] == '') or (cur ~= false and cur == ARGV[1]) then
	redis.call('SET', KEYS[1], ARGV[2], 'EX', ARGV[3])
	return 1
end
return 0`)

// CASDocument is a string record protected by compare-and-swap: a write lands only when the stored value still equals the value last read.
type CASDocument struct {
	Key string
	TTL time.Duration
}

// Read returns the document's raw bytes, or "" when it is absent.
func (d CASDocument) Read(ctx context.Context, client valkey_go.Client) (string, error) {
	raw, err := client.Do(ctx, client.B().Get().Key(d.Key).Build()).AsBytes()
	if err != nil {
		if valkey_go.IsValkeyNil(err) {
			return "", nil
		}
		return "", err
	}
	return string(raw), nil
}

// Swap writes newRaw only if the stored value still equals expectedRaw (empty meaning absent), and reports whether it did.
func (d CASDocument) Swap(ctx context.Context, client valkey_go.Client, expectedRaw, newRaw string) (bool, error) {
	n, err := casDocumentScript.Exec(ctx, client, []string{d.Key}, []string{
		expectedRaw, newRaw, strconv.FormatInt(int64(d.TTL.Seconds()), 10),
	}).AsInt64()
	if err != nil {
		return false, err
	}
	return n == 1, nil
}
