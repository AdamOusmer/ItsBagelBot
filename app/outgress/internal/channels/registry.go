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

	"github.com/valkey-io/valkey-go"
)

const (
	keyPrefix = "outgress:channel:"
	indexKey  = "outgress:channels"
	pausedKey = "outgress:paused"
)

type Channel struct {
	BroadcasterID string    `json:"broadcaster_id"`
	Enabled       bool      `json:"enabled"`
	IsMod         bool      `json:"is_mod"`
	ModCheckedAt  time.Time `json:"mod_checked_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

type Registry struct {
	client valkey.Client
}

func New(client valkey.Client) *Registry {
	return &Registry{client: client}
}

// Get returns the stored state for one broadcaster. found is false when the
// channel was never registered, in which case the caller should assume the
// defaults (enabled, non-mod).
func (r *Registry) Get(ctx context.Context, broadcasterID string) (Channel, bool, error) {

	fields, err := r.client.Do(ctx, r.client.B().Hgetall().Key(keyPrefix+broadcasterID).Build()).AsStrMap()
	if err != nil {
		return Channel{}, false, err
	}
	if len(fields) == 0 {
		return Channel{}, false, nil
	}

	return Channel{
		BroadcasterID: broadcasterID,
		Enabled:       fields["enabled"] == "1",
		IsMod:         fields["is_mod"] == "1",
		ModCheckedAt:  unixField(fields["mod_checked_at"]),
		UpdatedAt:     unixField(fields["updated_at"]),
	}, true, nil
}

// Save overwrites the full state of one channel and indexes it for List.
func (r *Registry) Save(ctx context.Context, ch Channel) error {

	key := keyPrefix + ch.BroadcasterID

	for _, res := range r.client.DoMulti(ctx,
		r.client.B().Hset().Key(key).FieldValue().
			FieldValue("enabled", boolField(ch.Enabled)).
			FieldValue("is_mod", boolField(ch.IsMod)).
			FieldValue("mod_checked_at", strconv.FormatInt(ch.ModCheckedAt.Unix(), 10)).
			FieldValue("updated_at", strconv.FormatInt(time.Now().Unix(), 10)).
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
			FieldValue("is_mod", boolField(isMod)).
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

// List returns every registered channel.
func (r *Registry) List(ctx context.Context) ([]Channel, error) {

	ids, err := r.client.Do(ctx, r.client.B().Smembers().Key(indexKey).Build()).AsStrSlice()
	if err != nil {
		return nil, err
	}

	out := make([]Channel, 0, len(ids))
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

func boolField(v bool) string {
	if v {
		return "1"
	}
	return "0"
}

func unixField(v string) time.Time {
	sec, err := strconv.ParseInt(v, 10, 64)
	if err != nil || sec <= 0 {
		return time.Time{}
	}
	return time.Unix(sec, 0)
}
