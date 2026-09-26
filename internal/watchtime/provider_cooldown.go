// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
package watchtime

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"
)

const providerCooldownPrefix = "watchtime:provider-reset:"
const providerResetMaximum = 24 * time.Hour

// providerCooldownKey is scoped to the actual provider identity, shared by
// every tenant and worker using that token. It supplements the token budget;
// it never creates another competing rate limiter.
func providerCooldownKey(identity string) (string, error) {
	if identity == "helix:app" {
		return providerCooldownPrefix + identity, nil
	}
	id, ok := strings.CutPrefix(identity, "helix:bot:")
	if !ok || id == "" || len(id) > 20 {
		return "", errors.New("invalid watch provider identity")
	}
	n, err := strconv.ParseUint(id, 10, 64)
	if err != nil || n == 0 || strconv.FormatUint(n, 10) != id {
		return "", errors.New("invalid watch provider identity")
	}
	return providerCooldownPrefix + identity, nil
}

const observeProviderResetScript = `
local clock=redis.call('TIME')
local now=tonumber(clock[1])*1000+math.floor(tonumber(clock[2])/1000)
local incoming=math.min(tonumber(ARGV[1]),now+tonumber(ARGV[2]))
local raw=redis.call('GET',KEYS[1])
local previous=raw and tonumber(raw) or 0
if raw and not tonumber(raw) then return redis.error_reply('invalid provider reset state') end
if previous>incoming then incoming=previous end
if incoming<=now then redis.call('DEL',KEYS[1]); return 0 end
redis.call('SET',KEYS[1],string.format('%.0f',incoming))
return 1`

// ObserveProviderReset publishes a provider 429 reset fleet-wide. Shorter or
// expired observations cannot release a cooldown another replica established.
// The server clock bounds resets to one day. Keys deliberately have no TTL:
// volatile-lru must never evict a provider reset early. There are only the
// actual bot/app identities; reads lazily retire elapsed resets. Authority
// errors propagate and must fail closed.
func (s *Store) ObserveProviderReset(ctx context.Context, identity string, reset time.Time) error {
	key, err := providerCooldownKey(identity)
	if err != nil {
		return err
	}
	if reset.IsZero() {
		return nil
	}
	return s.client.Do(ctx, s.client.B().Eval().Script(observeProviderResetScript).Numkeys(1).Key(key).Arg(strconv.FormatInt(reset.UnixMilli(), 10), strconv.FormatInt(providerResetMaximum.Milliseconds(), 10)).Build()).Error()
}

// ProviderRetryAt returns zero when no live provider cooldown exists. Read
// errors are not interpreted as absence: callers must stop the HTTP attempt.
func (s *Store) ProviderRetryAt(ctx context.Context, identity string) (time.Time, error) {
	key, err := providerCooldownKey(identity)
	if err != nil {
		return time.Time{}, err
	}
	n, err := s.client.Do(ctx, s.client.B().Eval().Script(`local raw=redis.call('GET',KEYS[1]); if not raw then return 0 end
local at=tonumber(raw); if not at or at<=0 then return redis.error_reply('invalid provider reset state') end
local clock=redis.call('TIME'); local now=tonumber(clock[1])*1000+math.floor(tonumber(clock[2])/1000)
if at<=now then redis.call('DEL',KEYS[1]); return 0 end
return at`).Numkeys(1).Key(key).Build()).AsInt64()
	if err != nil {
		return time.Time{}, err
	}
	if n == 0 {
		return time.Time{}, nil
	}
	return time.UnixMilli(n), nil
}
