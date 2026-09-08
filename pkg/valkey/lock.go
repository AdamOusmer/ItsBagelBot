// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package valkey

import (
	"context"
	"time"

	valkey_go "github.com/valkey-io/valkey-go"
)

// This file is a facade over the two Valkey mutual-exclusion idioms the fleet
// actually uses, and nothing else: an owner-scoped lock that is handed back
// explicitly (OwnerLock), and a fire-and-forget claim that is only ever
// released by its own expiry (ClaimOnce).
//
// Both were open-coded per package. The compare-and-delete script had three
// byte-identical copies (outgress token lease, batch store, channel registry)
// and the SET NX reply handling had nine, which meant a correction to either
// had to be made in every copy to be made at all. The subtle half is the reply
// handling: valkey-go surfaces the nil bulk string that a declined NX answers
// with as a Nil ERROR, so "someone else holds it" arrives on the error path
// and a naive `if err != nil { return err }` reports a lost race as an outage.
//
// Deliberately NOT one Lock interface with two implementations: the idioms
// differ in what the caller must do on the way out, which is precisely what a
// shared interface would hide. Callers pick one by name.
//
// Neither pins the client to the primary. SET NX is a write, so the shared
// client routes it to the Sentinel-elected master on its own; a pin buys
// consistency only for a read-back, and neither idiom reads back. Callers that
// read their own state around the lock (see outgress' ValkeyBatchStore) pin
// their own client and pass the pinned view in.

// claimValue is the placeholder a ClaimOnce key stores. It is never read back
// — presence alone is the claim — so it stays one byte.
const claimValue = "1"

// releaseIfOwner deletes the key only while its value is still ours. A plain
// DEL was rejected: a holder whose TTL lapsed mid-work would delete a lock a
// DIFFERENT replica has since taken, promoting one late worker into two
// concurrent ones, which is the exact failure the lock exists to prevent.
const releaseIfOwner = `if redis.call('get',KEYS[1])==ARGV[1] then return redis.call('del',KEYS[1]) else return 0 end`

// OwnerLock is one owner-scoped distributed lock: the client, the key it locks
// and the token it claims that key under. The three are fixed for a lock's
// lifetime, so bundling them lets Acquire take only what varies per call (the
// TTL) and Release take nothing, instead of threading all three through both.
//
// Use it when the holder finishes its work and hands the lock back early
// (releasing turns the TTL into a crash bound rather than the normal wait).
// When nothing is ever handed back, ClaimOnce is the smaller tool.
type OwnerLock struct {
	client valkey_go.Client
	key    string
	owner  string
}

// NewOwnerLock builds a lock on key held under owner. owner must identify THIS
// holder (a pod-unique or attempt-unique token): Release compares against it,
// so a value shared between replicas would let one release another's lock.
func NewOwnerLock(client valkey_go.Client, key, owner string) OwnerLock {
	return OwnerLock{client: client, key: key, owner: owner}
}

// Acquire takes the lock for ttl with SET NX PX. A false return with a nil
// error means another holder has it — a normal outcome, not a failure. A
// non-nil error is the backend refusing to answer; the caller decides whether
// that means back off or proceed uncoordinated.
func (l OwnerLock) Acquire(ctx context.Context, ttl time.Duration) (bool, error) {
	cmd := l.client.B().Set().Key(l.key).Value(l.owner).Nx().PxMilliseconds(ttl.Milliseconds()).Build()
	return wonClaim(l.client.Do(ctx, cmd))
}

// Release hands the lock back, but only while this owner still holds it (see
// releaseIfOwner). Releasing a lock that already expired, or that another
// replica has taken since, is a no-op rather than an error.
func (l OwnerLock) Release(ctx context.Context) error {
	cmd := l.client.B().Eval().Script(releaseIfOwner).Numkeys(1).Key(l.key).Arg(l.owner).Build()
	return l.client.Do(ctx, cmd).Error()
}

// ClaimOnce takes a self-expiring claim on key for ttl with SET NX EX: exactly
// one caller fleet-wide wins, and nobody releases — the claim lapses on its
// own. It is the "one replica handles this expiry / this sweep tick" idiom,
// where a second attempt within ttl is a duplicate to drop, not a caller
// waiting for a turn.
//
// ttl is truncated to whole seconds, matching every caller's own arming
// granularity; a sub-second ttl would floor to 0 and Valkey rejects that, so
// callers with a sub-second window want OwnerLock's PX instead.
func ClaimOnce(ctx context.Context, client valkey_go.Client, key string, ttl time.Duration) (bool, error) {
	cmd := client.B().Set().Key(key).Value(claimValue).Nx().Ex(ttl).Build()
	return wonClaim(client.Do(ctx, cmd))
}

// wonClaim reads a SET NX reply into (won, backend failure). The Nil split is
// the whole point: valkey-go reports the nil bulk that a declined NX answers
// with as an error, so a lost race must be peeled off the error path before
// anything left there can be treated as an outage.
func wonClaim(res valkey_go.ValkeyResult) (bool, error) {
	str, err := res.ToString()
	if err != nil {
		if valkey_go.IsValkeyNil(err) {
			return false, nil
		}
		return false, err
	}
	return str == "OK", nil
}
