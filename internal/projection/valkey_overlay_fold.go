// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package projection

import (
	"context"
	"strconv"
	"time"

	"ItsBagelBot/internal/domain/event/data"
	overlaysrpc "ItsBagelBot/internal/domain/rpc/overlays"
	"ItsBagelBot/internal/utils"
	"ItsBagelBot/pkg/cache"
	"ItsBagelBot/pkg/codec"
	pkg_valkey "ItsBagelBot/pkg/valkey"
)

const (
	overlayIDPrefix   = "overlay:o:id:"
	overlayHashPrefix = "overlay:o:hash:"
	overlayCacheTTL   = time.Minute
)

var overlayDataSeconds = strconv.FormatInt(int64(overlayCacheTTL/time.Second), 10)

var foldOverlayScript = pkg_valkey.NewScript(`
local revision = tonumber(redis.call('HGET', KEYS[1], 'revision') or '-1')
local incoming = tonumber(ARGV[1])
if incoming < revision then return 0 end
if incoming == revision and redis.call('HGET', KEYS[1], 'deleted') == '1' then return 0 end
local oldHash = redis.call('HGET', KEYS[1], 'hash') or ''
if oldHash ~= '' and (oldHash ~= ARGV[2] or ARGV[5] == '1') then
  redis.call('DEL', ARGV[6] .. oldHash)
end
redis.call('HSET', KEYS[1], 'revision', ARGV[1], 'deleted', ARGV[5])
redis.call('EXPIRE', KEYS[1], ARGV[8])
if ARGV[5] == '1' then
  redis.call('HDEL', KEYS[1], 'overlay', 'hash')
  return 1
end
redis.call('HSET', KEYS[1], 'hash', ARGV[2], 'overlay', ARGV[3])
redis.call('SET', KEYS[2], ARGV[4], 'EX', ARGV[7])
return 1`)

type cachedOverlay struct {
	Overlay   overlaysrpc.Overlay `json:"overlay"`
	ExpiresAt int64               `json:"expires_at"`
}

func (v *Store) FoldOverlay(ctx context.Context, change data.OverlayChangedDTO) (bool, error) {
	defer segment(ctx, "EVALSHA")()
	o := change.Overlay
	body, err := codec.Marshal(cachedOverlay{Overlay: o, ExpiresAt: time.Now().Add(overlayCacheTTL).Unix()})
	if err != nil {
		return false, err
	}
	keys := []string{cache.UserKey(overlayIDPrefix, o.ID), overlayHashPrefix + string(o.TokenHash)}
	args := []string{
		strconv.FormatUint(o.Revision, 10), string(o.TokenHash), string(body), strconv.FormatUint(o.ID, 10),
		utils.BoolField(change.Deleted), overlayHashPrefix, overlayDataSeconds, strconv.FormatInt(int64(DefaultTTL/time.Second), 10),
	}
	n, err := foldOverlayScript.Exec(ctx, v.primary, keys, args).AsInt64()
	return n == 1, err
}

func (v *Store) OverlayByHash(ctx context.Context, hash overlaysrpc.TokenHash) (overlaysrpc.Overlay, bool, error) {
	raw, found, err := pkg_valkey.ReadString(ctx, v.primary, overlayHashPrefix+string(hash))
	if err != nil || !found {
		return overlaysrpc.Overlay{}, false, err
	}
	id, err := strconv.ParseUint(raw, 10, 64)
	if err != nil {
		return overlaysrpc.Overlay{}, false, err
	}
	defer segment(ctx, "HGET")()
	cached, found, err := decodeJSONField[cachedOverlay](pkg_valkey.Exec1(ctx, v.primary, pkg_valkey.HGetCmd(v.primary, cache.UserKey(overlayIDPrefix, id), "overlay")))
	live := found && cached.ExpiresAt > time.Now().Unix() && cached.Overlay.TokenHash == hash
	return cached.Overlay, live, err
}
