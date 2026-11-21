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
	ttsUserPrefix = "overlay:tts:user:"
	ttsHashPrefix = "overlay:tts:hash:"
	ttsCacheTTL   = time.Minute
)

var (
	ttsDataSeconds    = strconv.FormatInt(int64(ttsCacheTTL/time.Second), 10)
	ttsVersionSeconds = strconv.FormatInt(int64(DefaultTTL/time.Second), 10)
)

var foldTtsScript = pkg_valkey.NewScript(`
local created = tonumber(redis.call('HGET', KEYS[1], 'created_at') or '0')
local revision = tonumber(redis.call('HGET', KEYS[1], 'revision') or '0')
local incomingCreated = tonumber(ARGV[1])
local incomingRevision = tonumber(ARGV[2])
if incomingCreated < created then return 0 end
if incomingCreated == created then
  if incomingRevision < revision then return 0 end
  if incomingRevision == revision and redis.call('HGET', KEYS[1], 'deleted') == '1' then return 0 end
end
local oldHash = redis.call('HGET', KEYS[1], 'hash') or ''
if oldHash ~= '' and (oldHash ~= ARGV[3] or ARGV[6] == '1') then
  redis.call('DEL', ARGV[7] .. oldHash)
end
redis.call('HSET', KEYS[1], 'created_at', ARGV[1], 'revision', ARGV[2], 'deleted', ARGV[6])
if ARGV[6] == '1' then
  redis.call('HDEL', KEYS[1], 'overlay', 'hash')
  redis.call('DEL', KEYS[2])
  redis.call('EXPIRE', KEYS[1], ARGV[9])
  return 1
end
redis.call('HSET', KEYS[1], 'hash', ARGV[3], 'overlay', ARGV[4])
redis.call('EXPIRE', KEYS[1], ARGV[9])
if ARGV[3] ~= '' then redis.call('SET', KEYS[2], ARGV[5], 'EX', ARGV[8]) end
return 1`)

func (v *Store) FoldTtsOverlay(ctx context.Context, change data.TtsOverlayChangedDTO) (bool, error) {
	defer segment(ctx, "EVALSHA")()
	o := change.Overlay
	body, err := codec.Marshal(cachedTtsOverlay{Overlay: o, ExpiresAt: time.Now().Add(ttsCacheTTL).Unix()})
	if err != nil {
		return false, err
	}
	keys := []string{cache.UserKey(ttsUserPrefix, o.UserID), ttsHashPrefix + string(o.TokenHash)}
	args := []string{
		strconv.FormatInt(o.CreatedAt, 10), strconv.FormatUint(o.Revision, 10), string(o.TokenHash),
		string(body), strconv.FormatUint(o.UserID, 10), utils.BoolField(change.Deleted), ttsHashPrefix, ttsDataSeconds, ttsVersionSeconds,
	}
	n, err := foldTtsScript.Exec(ctx, v.primary, keys, args).AsInt64()
	return n == 1, err
}

type cachedTtsOverlay struct {
	Overlay   overlaysrpc.TtsOverlay `json:"overlay"`
	ExpiresAt int64                  `json:"expires_at"`
}

func (v *Store) TtsOverlay(ctx context.Context, userID uint64) (overlaysrpc.TtsOverlay, bool, error) {
	defer segment(ctx, "HGET")()
	cached, found, err := decodeJSONField[cachedTtsOverlay](pkg_valkey.Exec1(ctx, v.primary, pkg_valkey.HGetCmd(v.primary, cache.UserKey(ttsUserPrefix, userID), "overlay")))
	return cached.Overlay, found && cached.ExpiresAt > time.Now().Unix(), err
}

func (v *Store) TtsOverlayByHash(ctx context.Context, hash overlaysrpc.TtsTokenHash) (overlaysrpc.TtsOverlay, bool, error) {
	raw, found, err := pkg_valkey.ReadString(ctx, v.primary, ttsHashPrefix+string(hash))
	if err != nil || !found {
		return overlaysrpc.TtsOverlay{}, false, err
	}
	id, err := strconv.ParseUint(raw, 10, 64)
	if err != nil {
		return overlaysrpc.TtsOverlay{}, false, err
	}
	overlay, found, err := v.TtsOverlay(ctx, id)
	return overlay, found && overlay.TokenHash == hash, err
}
