// Package channels keeps the managed per-broadcaster state of the egress
// path in Valkey: whether a channel receives traffic at all, and whether the
// bot moderates it (which doubles its Twitch chat allowance). The registry
// is written through the management RPC and by the workers' periodic mod
// verification; unknown channels default to enabled and non-mod, so the safe
// rate applies until someone or something says otherwise.
package channels

import (
	"context"
	"strconv"
	"time"

	"ItsBagelBot/internal/domain/rpc/manage"
	"ItsBagelBot/internal/utils"

	"github.com/valkey-io/valkey-go"
)

const (
	keyPrefix = "outgress:channel:"
	indexKey  = "outgress:channels"
	pausedKey = "outgress:paused"
)

type Registry struct {
	client valkey.Client
}

func New(client valkey.Client) *Registry {
	return &Registry{client: client}
}

// Get returns the stored state for one broadcaster. found is false when the
// channel was never registered, in which case the caller should assume the
// defaults (enabled, non-mod).
func (r *Registry) Get(ctx context.Context, broadcasterID string) (manage.Channel, bool, error) {

	fields, err := r.client.Do(ctx, r.client.B().Hgetall().Key(keyPrefix+broadcasterID).Build()).AsStrMap()
	if err != nil {
		return manage.Channel{}, false, err
	}
	if len(fields) == 0 {
		return manage.Channel{}, false, nil
	}

	return manage.Channel{
		BroadcasterID: broadcasterID,
		Enabled:       fields["enabled"] == "1",
		IsMod:         fields["is_mod"] == "1",
		ModCheckedAt:  unixField(fields["mod_checked_at"]),
		UpdatedAt:     unixField(fields["updated_at"]),
		SubState:      fields["sub_state"],
		SubError:      fields["sub_error"],
		SubCheckedAt:  unixField(fields["sub_checked_at"]),
	}, true, nil
}

// Save overwrites the full state of one channel and indexes it for List.
func (r *Registry) Save(ctx context.Context, ch manage.Channel) error {

	key := keyPrefix + ch.BroadcasterID

	modCheckedAt := "0"
	if !ch.ModCheckedAt.IsZero() {
		modCheckedAt = strconv.FormatInt(ch.ModCheckedAt.Unix(), 10)
	}
	subCheckedAt := "0"
	if !ch.SubCheckedAt.IsZero() {
		subCheckedAt = strconv.FormatInt(ch.SubCheckedAt.Unix(), 10)
	}

	for _, res := range r.client.DoMulti(ctx,
		r.client.B().Hset().Key(key).FieldValue().
			FieldValue("enabled", utils.BoolField(ch.Enabled)).
			FieldValue("is_mod", utils.BoolField(ch.IsMod)).
			FieldValue("mod_checked_at", modCheckedAt).
			FieldValue("updated_at", strconv.FormatInt(time.Now().Unix(), 10)).
			FieldValue("sub_state", ch.SubState).
			FieldValue("sub_error", ch.SubError).
			FieldValue("sub_checked_at", subCheckedAt).
			Build(),
		r.client.B().Sadd().Key(indexKey).Member(ch.BroadcasterID).Build(),
	) {
		if err := res.Error(); err != nil {
			return err
		}
	}

	return nil
}

// SetMod records a verified mod status without touching the enabled flag;
// a channel first seen through verification starts out enabled.
func (r *Registry) SetMod(ctx context.Context, broadcasterID string, isMod bool) error {

	key := keyPrefix + broadcasterID
	now := strconv.FormatInt(time.Now().Unix(), 10)

	for _, res := range r.client.DoMulti(ctx,
		r.client.B().Hsetnx().Key(key).Field("enabled").Value("1").Build(),
		r.client.B().Hset().Key(key).FieldValue().
			FieldValue("is_mod", utils.BoolField(isMod)).
			FieldValue("mod_checked_at", now).
			FieldValue("updated_at", now).
			Build(),
		r.client.B().Sadd().Key(indexKey).Member(broadcasterID).Build(),
	) {
		if err := res.Error(); err != nil {
			return err
		}
	}

	return nil
}

// SetSubState records the current eventsub enrollment state for a broadcaster
// without touching enabled/is_mod. It also updates updated_at so listeners
// polling the registry see a freshness bump.
func (r *Registry) SetSubState(ctx context.Context, broadcasterID, state, errMsg string) error {

	key := keyPrefix + broadcasterID
	now := strconv.FormatInt(time.Now().Unix(), 10)

	for _, res := range r.client.DoMulti(ctx,
		r.client.B().Hset().Key(key).FieldValue().
			FieldValue("sub_state", state).
			FieldValue("sub_error", errMsg).
			FieldValue("sub_checked_at", now).
			FieldValue("updated_at", now).
			Build(),
		r.client.B().Sadd().Key(indexKey).Member(broadcasterID).Build(),
	) {
		if err := res.Error(); err != nil {
			return err
		}
	}

	return nil
}

const enrollLockPrefix = "outgress:enroll:lock:"

// AcquireEnrollLock tries to set a Valkey NX key as a distributed lock.
// Returns true if this caller owns the lock, false if another replica holds it.
func (r *Registry) AcquireEnrollLock(ctx context.Context, broadcasterID, owner string, ttl time.Duration) (bool, error) {

	key := enrollLockPrefix + broadcasterID
	res := r.client.Do(ctx,
		r.client.B().Set().Key(key).Value(owner).Nx().PxMilliseconds(ttl.Milliseconds()).Build(),
	)
	// SET NX returns a bulk-string "OK" on success or a nil bulk-string on failure.
	// rueidis/valkey-go represents the nil bulk as a Nil error on ToString.
	str, err := res.ToString()
	if err != nil {
		if valkey.IsValkeyNil(err) {
			return false, nil
		}
		return false, err
	}
	return str == "OK", nil
}

// ReleaseEnrollLock deletes the lock key only when its value matches owner,
// preventing a replica from releasing a lock it no longer holds.
func (r *Registry) ReleaseEnrollLock(ctx context.Context, broadcasterID, owner string) error {

	key := enrollLockPrefix + broadcasterID
	const luaDel = `if redis.call('get',KEYS[1])==ARGV[1] then return redis.call('del',KEYS[1]) else return 0 end`
	return r.client.Do(ctx,
		r.client.B().Eval().Script(luaDel).Numkeys(1).Key(key).Arg(owner).Build(),
	).Error()
}

// List returns every registered channel.
func (r *Registry) List(ctx context.Context) ([]manage.Channel, error) {

	ids, err := r.client.Do(ctx, r.client.B().Smembers().Key(indexKey).Build()).AsStrSlice()
	if err != nil {
		return nil, err
	}

	out := make([]manage.Channel, 0, len(ids))
	for _, id := range ids {
		ch, found, err := r.Get(ctx, id)
		if err != nil {
			return nil, err
		}
		if found {
			out = append(out, ch)
		}
	}

	return out, nil
}

// SetPaused flips the global kill switch. While paused, workers nack every
// message; redelivery pacing holds them for a while, but messages older
// than the retry budget are dropped, which is the right call for chat.
func (r *Registry) SetPaused(ctx context.Context, paused bool) error {

	if paused {
		return r.client.Do(ctx, r.client.B().Set().Key(pausedKey).Value("1").Build()).Error()
	}

	return r.client.Do(ctx, r.client.B().Del().Key(pausedKey).Build()).Error()
}

// Paused reports the state of the global kill switch.
func (r *Registry) Paused(ctx context.Context) (bool, error) {

	res, err := r.client.Do(ctx, r.client.B().Get().Key(pausedKey).Build()).ToString()
	if err != nil {
		if valkey.IsValkeyNil(err) {
			return false, nil
		}
		return false, err
	}

	return res == "1", nil
}

func unixField(v string) time.Time {
	sec, err := strconv.ParseInt(v, 10, 64)
	if err != nil || sec <= 0 {
		return time.Time{}
	}
	return time.Unix(sec, 0)
}
