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

var casDocumentOnceScript = valkey_go.NewLuaScript(`
if redis.call('EXISTS', KEYS[2]) == 1 then return 2 end
local cur = redis.call('GET', KEYS[1])
if (cur == false and ARGV[1] == '') or (cur ~= false and cur == ARGV[1]) then
	redis.call('SET', KEYS[1], ARGV[2], 'EX', ARGV[3])
	redis.call('SET', KEYS[2], '1', 'PX', ARGV[4])
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

// Update runs change on the current raw value ("" when absent) and swaps in its result, rereading and retrying while another writer wins.
func (d CASDocument) Update(ctx context.Context, client valkey_go.Client, change func(raw string) (string, error)) error {
	for {
		raw, err := d.Read(ctx, client)
		if err != nil {
			return err
		}
		next, err := change(raw)
		if err != nil {
			return err
		}
		swapped, err := d.Swap(ctx, client, raw, next)
		if err != nil || swapped {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
	}
}

// The receipt key must share the document's cluster slot.
func (d CASDocument) UpdateOnce(ctx context.Context, client valkey_go.Client, receipt Key, change func(raw string) (string, error)) (bool, error) {
	for {
		raw, err := d.Read(ctx, client)
		if err != nil {
			return false, err
		}
		next, err := change(raw)
		if err != nil {
			return false, err
		}
		n, err := casDocumentOnceScript.Exec(ctx, client, []string{d.Key, receipt.Name}, []string{
			raw, next, strconv.FormatInt(int64(d.TTL.Seconds()), 10), strconv.FormatInt(receipt.TTL.Milliseconds(), 10),
		}).AsInt64()
		if err != nil || n != 0 {
			return n == 1, err
		}
		if err := ctx.Err(); err != nil {
			return false, err
		}
	}
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
