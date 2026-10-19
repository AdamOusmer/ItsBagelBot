// Copyright (c) 2026 Adam Ousmer. All rights reserved.
package projection

import (
	"context"
	"strconv"
	"time"

	"ItsBagelBot/internal/domain/lifecycle"
	"ItsBagelBot/internal/utils"
	"ItsBagelBot/internal/watchtime"
	"ItsBagelBot/pkg/cache"
	"ItsBagelBot/pkg/codec"
	pkg_valkey "ItsBagelBot/pkg/valkey"

	"github.com/valkey-io/valkey-go"
)

func (v *Store) ClaimUnknownLive(ctx context.Context, id uint64) (bool, error) {
	return pkg_valkey.ClaimOnce(ctx, v.primary, "live:cold:"+strconv.FormatUint(id, 10), 30*time.Second)
}

// GetUserRuntime reads admission from the primary: a replica must not reopen
// execution after a restriction was written. Ordinary settings readers may cache it.
func (v *Store) GetUserRuntime(ctx context.Context, id uint64) (User, bool, error) {
	fields, err := v.primary.Do(ctx, v.primary.B().Hmget().Key(cache.UserKey(settingsKeyPrefix, id)).Field("status", "active", "banned", "locale", "commands_page_hidden", "account_created_at", "lifecycle", "leaderboard_page_hidden").Build()).AsStrSlice()
	if err != nil {
		return User{}, false, err
	}
	if len(fields) < 8 || fields[0] == "" {
		return User{}, false, nil
	}
	u := User{Status: fields[0], IsActive: fields[1] == "1", Banned: fields[2] == "1", Locale: fields[3], CommandsPageHidden: fields[4] == "1", LeaderboardPageHidden: fields[7] == "1"}
	u.AccountCreatedAt, _ = strconv.ParseInt(fields[5], 10, 64)
	if fields[6] != "" && fields[6] != "null" {
		if err := codec.Unmarshal([]byte(fields[6]), &u.Lifecycle); err != nil {
			return User{}, false, err
		}
	}
	return u, true, nil
}

// SetLifecycle makes an acknowledged owner transition visible before control
// processing returns. Full owner snapshots use the same revision fence.
type LifecycleProjection struct {
	State            lifecycle.State
	AccountCreatedAt int64
	StateRevision    int64
}

func (v *Store) SetLifecycle(ctx context.Context, id uint64, update LifecycleProjection) error {
	body, err := codec.Marshal(update.State)
	if err != nil {
		return err
	}
	keys := []string{cache.UserKey(settingsKeyPrefix, id), watchtime.AdmissionKey(id)}
	args := []string{strconv.FormatInt(update.AccountCreatedAt, 10), strconv.FormatInt(update.StateRevision, 10), string(body), utils.BoolField(update.State.AllowsRuntime()), utils.BoolField(update.State.AllowsBackground())}
	return runtimeWriteScript.Exec(ctx, v.primary, keys, args).Error()
}

var runtimeWriteScript = valkey.NewLuaScript(runtimeWrite)

const runtimeWrite = `
if redis.call('HGET',KEYS[2],'deleted')=='1' then return 0 end
local instance=redis.call('HGET',KEYS[2],'instance')
if instance and instance~=ARGV[1] then return 0 end
local revision=tonumber(redis.call('HGET',KEYS[2],'state_revision') or '0')
if tonumber(ARGV[2])<revision then return 0 end
local prior=redis.call('HGET',KEYS[1],'lifecycle')
local changed=true
if prior and prior~='null' then
 changed=cjson.decode(prior).admission_generation~=cjson.decode(ARGV[3]).admission_generation
end
if changed then redis.call('HINCRBY',KEYS[2],'epoch',1) end
redis.call('HSET',KEYS[2],'instance',ARGV[1],'state_revision',ARGV[2])
redis.call('HSET',KEYS[1],'account_created_at',ARGV[1],'lifecycle',ARGV[3],'active',ARGV[4])
redis.call('HSET',KEYS[1],'runtime_background',ARGV[5])
if redis.call('TTL',KEYS[1])<0 then redis.call('EXPIRE',KEYS[1],86400) end
return 1`
