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
)

func (v *Store) ClaimUnknownLive(ctx context.Context, id uint64) (bool, error) {
	return pkg_valkey.ClaimOnce(ctx, v.primary, "live:cold:"+strconv.FormatUint(id, 10), 30*time.Second)
}

// GetUserRuntime reads admission from the primary: a replica must not reopen
// execution after a restriction was written. Ordinary settings readers may cache it.
func (v *Store) GetUserRuntime(ctx context.Context, id uint64) (User, bool, error) {
	fields, err := pkg_valkey.Exec1(ctx, v.primary, pkg_valkey.HMGetCmd(v.primary, cache.UserKey(settingsKeyPrefix, id), "status", "active", "banned", "locale", "commands_page_hidden", "account_created_at", "lifecycle", "leaderboard_page_hidden", "creator_code", "songqueue_page_hidden")).AsStrSlice()
	if err != nil {
		return User{}, false, err
	}
	if len(fields) < 10 || fields[0] == "" {
		return User{}, false, nil
	}
	u := User{Status: fields[0], IsActive: fields[1] == "1", Banned: fields[2] == "1", Locale: fields[3], CommandsPageHidden: fields[4] == "1", LeaderboardPageHidden: fields[7] == "1", CreatorCode: fields[8], SongQueuePageHidden: fields[9] == "1"}
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
	return pkg_valkey.RunHashRevisionGateRuntime(ctx, v.primary, keys, args).Error()
}
