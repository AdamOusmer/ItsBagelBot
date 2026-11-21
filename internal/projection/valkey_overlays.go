// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package projection

import (
	"context"
	"strconv"
	"time"

	overlaysrpc "ItsBagelBot/internal/domain/rpc/overlays"
	"ItsBagelBot/pkg/cache"
	"ItsBagelBot/pkg/codec"
	pkg_valkey "ItsBagelBot/pkg/valkey"
)

type PoweredOverlay = overlaysrpc.PoweredOverlay

const (
	poweredUserPrefix  = "overlay:powered:user:"
	poweredTokenPrefix = "overlay:powered:token:"
)

var (
	poweredTTLSeconds       = strconv.FormatInt(int64(DefaultTTL/time.Second), 10)
	poweredTombstoneSeconds = strconv.FormatInt(int64(10*time.Minute/time.Second), 10)
)

var poweredWriteScript = pkg_valkey.NewScript(`
local current = tonumber(redis.call('HGET', KEYS[1], 'revision') or '-1')
local incoming = tonumber(ARGV[1])
if incoming < current then return 0 end
if incoming == current and redis.call('HGET', KEYS[1], 'deleted') == '1' then return 0 end
redis.call('HDEL', KEYS[1], 'deleted')
redis.call('HSET', KEYS[1], 'revision', ARGV[1], 'token', ARGV[2], 'overlay', ARGV[3])
redis.call('EXPIRE', KEYS[1], ARGV[5])
redis.call('SET', KEYS[2], ARGV[4], 'EX', ARGV[5])
if KEYS[3] ~= KEYS[2] then redis.call('DEL', KEYS[3]) end
return 1`)

var poweredDeleteScript = pkg_valkey.NewScript(`
local current = tonumber(redis.call('HGET', KEYS[1], 'revision') or '-1')
local revision = math.max(current, tonumber(ARGV[1]))
local token = redis.call('HGET', KEYS[1], 'token') or ''
redis.call('DEL', KEYS[1])
redis.call('HSET', KEYS[1], 'revision', string.format('%d', revision), 'deleted', '1')
redis.call('EXPIRE', KEYS[1], ARGV[2])
for i = 2, #KEYS do redis.call('DEL', KEYS[i]) end
return token`)

type PoweredWrite struct {
	Overlay       PoweredOverlay
	PreviousToken string
}

type PoweredRemoval struct {
	UserID   uint64
	Revision uint64
	Tokens   []string
}

func poweredUserKey(userID uint64) string {
	return cache.UserKey(poweredUserPrefix, userID)
}

func poweredTokenKey(token string) string {
	return poweredTokenPrefix + token
}

func (v *Store) SetPoweredOverlay(ctx context.Context, w PoweredWrite) (bool, error) {
	defer segment(ctx, "EVALSHA")()
	body, err := codec.Marshal(w.Overlay)
	if err != nil {
		return false, err
	}
	current := poweredTokenKey(w.Overlay.Token)
	previous := current
	if w.PreviousToken != "" {
		previous = poweredTokenKey(w.PreviousToken)
	}
	keys := []string{poweredUserKey(w.Overlay.UserID), current, previous}
	args := []string{strconv.FormatUint(w.Overlay.Revision, 10), w.Overlay.Token, string(body), strconv.FormatUint(w.Overlay.UserID, 10), poweredTTLSeconds}
	n, err := poweredWriteScript.Exec(ctx, v.primary, keys, args).AsInt64()
	return n == 1, err
}

func (v *Store) DeletePoweredOverlay(ctx context.Context, r PoweredRemoval) ([]string, error) {
	defer segment(ctx, "EVALSHA")()
	keys := []string{poweredUserKey(r.UserID)}
	for _, token := range r.Tokens {
		keys = append(keys, poweredTokenKey(token))
	}
	stored, err := poweredDeleteScript.Exec(ctx, v.primary, keys, []string{strconv.FormatUint(r.Revision, 10), poweredTombstoneSeconds}).ToString()
	if err != nil || stored == "" {
		return r.Tokens, err
	}
	if err := pkg_valkey.Exec1(ctx, v.primary, pkg_valkey.DelCmd(v.primary, poweredTokenKey(stored))).Error(); err != nil {
		return nil, err
	}
	return append(r.Tokens, stored), nil
}

func (v *Store) PoweredOverlay(ctx context.Context, userID uint64) (PoweredOverlay, bool, error) {
	defer segment(ctx, "HGET")()
	return decodeJSONField[PoweredOverlay](pkg_valkey.Exec1(ctx, v.primary, pkg_valkey.HGetCmd(v.primary, poweredUserKey(userID), "overlay")))
}

// A rotated-away token must not resolve: the index is trusted only while the overlay still carries the token.
func (v *Store) PoweredOverlayByToken(ctx context.Context, token string) (PoweredOverlay, bool, error) {
	raw, found, err := pkg_valkey.ReadString(ctx, v.primary, poweredTokenKey(token))
	if err != nil || !found {
		return PoweredOverlay{}, false, err
	}
	userID, err := strconv.ParseUint(raw, 10, 64)
	if err != nil {
		return PoweredOverlay{}, false, nil
	}
	overlay, found, err := v.PoweredOverlay(ctx, userID)
	if overlay.Token != token {
		return PoweredOverlay{}, false, err
	}
	return overlay, found, err
}
