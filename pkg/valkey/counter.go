// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package valkey

import (
	"context"
	"time"

	valkey_go "github.com/valkey-io/valkey-go"
)

// Incr and GetInt are the fleet's one shared idiom for a Valkey integer
// counter that carries a rolling TTL: INCR then EXPIRE in one pipelined round
// trip, and a read that treats a missing key as zero rather than an error.
// ValkeyReputation.Bump (reputation_valkey.go) open-codes the same two
// commands, and the timers store (timer-conditions.md D5, chat lines and
// fires-per-stream) was about to become the third copy. D15 draws the line at
// the third copy rather than the second: refactoring Bump's existing inline
// DoMulti is a separate cleanup, out of scope here, so it stays as it is.
//
// Neither function pins the client to the primary. INCR and SET are writes,
// so the shared client routes them to the Sentinel-elected master on its own.

// Incr increments key by 1 and resets its TTL to ttl, in one DoMulti round
// trip. It returns the counter's new value. The EXPIRE re-applies on every
// call, including one that finds the key already alive with time left,
// because a counter whose TTL never refreshes on activity would lapse
// mid-stream under a broadcaster who is still live, the whole point of
// pairing INCR with EXPIRE instead of setting the TTL once at creation.
func Incr(ctx context.Context, client valkey_go.Client, key string, ttl time.Duration) (int64, error) {
	resps := client.DoMulti(ctx,
		client.B().Incr().Key(key).Build(),
		client.B().Expire().Key(key).Seconds(int64(ttl.Seconds())).Build(),
	)
	n, err := resps[0].AsInt64()
	if err != nil {
		return 0, err
	}
	if err := resps[1].Error(); err != nil {
		return 0, err
	}
	return n, nil
}

// GetInt reads key as an integer, treating a missing key as zero rather than
// an error. Every counter this backs (a chat-activity gate's line count, a
// fire cap's per-stream count) is meaningless before its first Incr, so a
// caller comparing against it should not have to special-case "never
// incremented" apart from "incremented, then expired": both are zero.
func GetInt(ctx context.Context, client valkey_go.Client, key string) (int64, error) {
	n, err := client.Do(ctx, client.B().Get().Key(key).Build()).AsInt64()
	if err != nil {
		if valkey_go.IsValkeyNil(err) {
			return 0, nil
		}
		return 0, err
	}
	return n, nil
}
