// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package projection

import (
	"context"
	"strconv"
	"time"

	livekey "ItsBagelBot/internal/domain/live"
	"ItsBagelBot/internal/utils"
	"ItsBagelBot/pkg/cache"
	pkg_valkey "ItsBagelBot/pkg/valkey"

	"github.com/valkey-io/valkey-go"
)

const (
	streamTitleField       = "stream:title"
	streamGameField        = "stream:game"
	streamViewersField     = "stream:viewers"
	streamPeakViewersField = "stream:peak_viewers"
	streamStartedAtField   = "stream:started_at"
	streamEndedAtField     = "stream:ended_at"

	streamCtrMessagesField  = "streamctr:messages"
	streamCtrAnsweredField  = "streamctr:answered"
	streamCtrModActionField = "streamctr:mod_actions"

	streamLiveField            = "live"
	streamLiveVersionField     = "live_ver"
	streamBaselinePendingField = "stream_baseline_pending_ver"
)

func streamUserKey(userID string) (string, error) {
	id, err := strconv.ParseUint(userID, 10, 64)
	if err != nil {
		return "", err
	}
	return cache.UserKey(settingsKeyPrefix, id), nil
}

type StreamLive struct {
	Live    bool
	Known   bool
	Version int64
}

type StreamLiveFold struct {
	Applied bool
	WasLive bool
}

type optionalInts struct {
	err error
}

func (o *optionalInts) read(res valkey.ValkeyResult) (value int64, present bool) {
	text, err := res.ToString()
	switch {
	case valkey.IsValkeyNil(err):
		return 0, false
	case err != nil:
		o.err = err
		return 0, false
	}
	value, _ = strconv.ParseInt(text, 10, 64)
	return value, true
}

// LiveBroadcasters is the set of live:<id> keys, read through pkg/valkey.
func (v *Store) LiveBroadcasters(ctx context.Context) ([]uint64, error) {
	return livekey.BroadcasterIDs(ctx, v.client)
}

// Newest version wins between the projected flag and the live key; either alone can be stale.
func (v *Store) GetStreamLive(ctx context.Context, userID uint64) (StreamLive, error) {
	defer segment(ctx, "HGET")()

	key := cache.UserKey(settingsKeyPrefix, userID)

	res := pkg_valkey.ExecMulti(ctx, v.client,
		pkg_valkey.HGetCmd(v.client, key, streamLiveField),
		pkg_valkey.HGetCmd(v.client, key, streamLiveVersionField),
		pkg_valkey.GetCmd(v.client, livekey.Key(userID)),
		pkg_valkey.GetCmd(v.client, livekey.VerKey(userID)),
	)

	var ints optionalInts
	projectedLive, projectedKnown := ints.read(res[0])
	projectedVersion, _ := ints.read(res[1])
	_, confirmedLive := ints.read(res[2])
	confirmedVersion, _ := ints.read(res[3])
	if ints.err != nil {
		return StreamLive{}, ints.err
	}

	if confirmedVersion > projectedVersion {
		return StreamLive{Live: confirmedLive, Known: true, Version: confirmedVersion}, nil
	}
	return StreamLive{Live: projectedLive == 1, Known: projectedKnown, Version: projectedVersion}, nil
}

func (v *Store) SetStreamLive(ctx context.Context, userID uint64, next StreamLive) (StreamLiveFold, error) {
	defer segment(ctx, "EVALSHA")()

	prior, err := pkg_valkey.RunVersionFoldApply(ctx, v.primary, []string{cache.UserKey(settingsKeyPrefix, userID)}, []string{
		utils.BoolField(next.Live), strconv.FormatInt(next.Version, 10), strconv.FormatInt(int64(DefaultTTL/time.Second), 10),
	}).AsInt64()
	if err != nil {
		return StreamLiveFold{}, err
	}
	return StreamLiveFold{Applied: prior >= 0, WasLive: prior == 1}, nil
}

type StreamInfo struct {
	Title       string
	GameName    string
	ViewerCount int
	PeakViewers int
	StartedAt   time.Time
	EndedAt     time.Time
}

func (v *Store) SetStreamInfo(ctx context.Context, userID string, info StreamInfo) error {
	defer segment(ctx, "HSET")()

	key, err := streamUserKey(userID)
	if err != nil {
		return err
	}

	return v.pipelineWithTTL(ctx, key, DefaultTTL,
		pkg_valkey.HSetCmd(v.client, key,
			pkg_valkey.HashFieldValue{Field: streamTitleField, Value: info.Title},
			pkg_valkey.HashFieldValue{Field: streamGameField, Value: info.GameName},
			pkg_valkey.HashFieldValue{Field: streamViewersField, Value: strconv.Itoa(info.ViewerCount)},
			pkg_valkey.HashFieldValue{Field: streamPeakViewersField, Value: strconv.Itoa(info.PeakViewers)},
			pkg_valkey.HashFieldValue{Field: streamStartedAtField, Value: info.StartedAt.Format(time.RFC3339)},
			pkg_valkey.HashFieldValue{Field: streamEndedAtField, Value: info.EndedAt.Format(time.RFC3339)},
		),
	)
}

func (v *Store) GetStreamInfo(ctx context.Context, userID string) (StreamInfo, bool, error) {
	defer segment(ctx, "HGET")()

	key, err := streamUserKey(userID)
	if err != nil {
		return StreamInfo{}, false, err
	}

	res := pkg_valkey.ExecMulti(ctx, v.client,
		pkg_valkey.HGetCmd(v.client, key, streamTitleField),
		pkg_valkey.HGetCmd(v.client, key, streamGameField),
		pkg_valkey.HGetCmd(v.client, key, streamViewersField),
		pkg_valkey.HGetCmd(v.client, key, streamPeakViewersField),
		pkg_valkey.HGetCmd(v.client, key, streamStartedAtField),
		pkg_valkey.HGetCmd(v.client, key, streamEndedAtField),
	)

	title, err := res[0].ToString()
	if err != nil {
		if valkey.IsValkeyNil(err) {
			return StreamInfo{}, false, nil
		}
		return StreamInfo{}, false, err
	}

	game, _ := res[1].ToString()
	viewers, _ := res[2].ToString()
	peak, _ := res[3].ToString()
	startedAt, _ := res[4].ToString()
	endedAt, _ := res[5].ToString()

	viewerCount, _ := strconv.Atoi(viewers)
	peakViewers, _ := strconv.Atoi(peak)
	started, _ := time.Parse(time.RFC3339, startedAt)
	ended, _ := time.Parse(time.RFC3339, endedAt)

	return StreamInfo{
		Title:       title,
		GameName:    game,
		ViewerCount: viewerCount,
		PeakViewers: peakViewers,
		StartedAt:   started,
		EndedAt:     ended,
	}, true, nil
}

type StreamCounters struct {
	Messages   int64
	Answered   int64
	ModActions int64
}

func (v *Store) SetStreamCounterBaseline(ctx context.Context, userID string, b StreamCounters) error {
	defer segment(ctx, "HSET")()

	key, err := streamUserKey(userID)
	if err != nil {
		return err
	}

	return v.pipelineWithTTL(ctx, key, DefaultTTL,
		pkg_valkey.HSetCmd(v.client, key,
			pkg_valkey.HashFieldValue{Field: streamCtrMessagesField, Value: strconv.FormatInt(b.Messages, 10)},
			pkg_valkey.HashFieldValue{Field: streamCtrAnsweredField, Value: strconv.FormatInt(b.Answered, 10)},
			pkg_valkey.HashFieldValue{Field: streamCtrModActionField, Value: strconv.FormatInt(b.ModActions, 10)},
		),
	)
}

func (v *Store) GetStreamCounterBaseline(ctx context.Context, userID string) (StreamCounters, bool, error) {
	defer segment(ctx, "HGET")()

	key, err := streamUserKey(userID)
	if err != nil {
		return StreamCounters{}, false, err
	}

	res := pkg_valkey.ExecMulti(ctx, v.primary,
		pkg_valkey.HGetCmd(v.primary, key, streamCtrMessagesField),
		pkg_valkey.HGetCmd(v.primary, key, streamCtrAnsweredField),
		pkg_valkey.HGetCmd(v.primary, key, streamCtrModActionField),
	)

	messages, err := res[0].ToString()
	if err != nil {
		if valkey.IsValkeyNil(err) {
			return StreamCounters{}, false, nil
		}
		return StreamCounters{}, false, err
	}

	answered, _ := res[1].ToString()
	modActions, _ := res[2].ToString()

	msgs, _ := strconv.ParseInt(messages, 10, 64)
	ans, _ := strconv.ParseInt(answered, 10, 64)
	mods, _ := strconv.ParseInt(modActions, 10, 64)

	return StreamCounters{Messages: msgs, Answered: ans, ModActions: mods}, true, nil
}

// StreamBaselinePending survives a lifecycle read failure after accepting online.
// Its version remains tied to the latest accepted live fact, never wall-clock replay.
func (v *Store) StreamBaselinePending(ctx context.Context, id uint64, version int64) (bool, error) {
	key := cache.UserKey(settingsKeyPrefix, id)
	value, err := pkg_valkey.Exec1(ctx, v.primary, pkg_valkey.HGetCmd(v.primary, key, streamBaselinePendingField)).AsInt64()
	if valkey.IsValkeyNil(err) {
		return false, nil
	}
	return value == version, err
}
func (v *Store) DiscardStreamBaseline(ctx context.Context, id uint64, version int64) error {
	return pkg_valkey.RunVersionFoldDiscardPending(ctx, v.primary, []string{cache.UserKey(settingsKeyPrefix, id)}, []string{strconv.FormatInt(version, 10)}).Error()
}
func (v *Store) SetStreamCounterBaselineForVersion(ctx context.Context, id uint64, version int64, b StreamCounters) error {
	keys := []string{cache.UserKey(settingsKeyPrefix, id), livekey.VerKey(id)}
	args := []string{strconv.FormatInt(version, 10), strconv.FormatInt(b.Messages, 10), strconv.FormatInt(b.Answered, 10), strconv.FormatInt(b.ModActions, 10)}
	return pkg_valkey.RunVersionFoldCommitPending(ctx, v.primary, keys, args).Error()
}
