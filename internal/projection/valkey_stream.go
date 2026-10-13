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

// Newest version wins between the projected flag and the live key; either alone can be stale.
func (v *Store) GetStreamLive(ctx context.Context, userID uint64) (StreamLive, error) {
	defer segment(ctx, "HGET")()

	key := cache.UserKey(settingsKeyPrefix, userID)

	res := v.client.DoMulti(ctx,
		v.client.B().Hget().Key(key).Field(streamLiveField).Build(),
		v.client.B().Hget().Key(key).Field(streamLiveVersionField).Build(),
		v.client.B().Get().Key(livekey.Key(userID)).Build(),
		v.client.B().Get().Key(livekey.VerKey(userID)).Build(),
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

var foldStreamLiveScript = valkey.NewLuaScript(`local cur = tonumber(redis.call('HGET', KEYS[1], 'live_ver'))
if cur and cur > tonumber(ARGV[2]) then return -1 end
local was = redis.call('HGET', KEYS[1], 'live')
if ARGV[1]=='1' then
 if was~='1' or redis.call('HEXISTS',KEYS[1],'stream_baseline_pending_ver')==1 then redis.call('HSET',KEYS[1],'stream_baseline_pending_ver',ARGV[2]) end
else redis.call('HDEL',KEYS[1],'stream_baseline_pending_ver') end
redis.call('HSET', KEYS[1], 'live', ARGV[1], 'live_ver', ARGV[2])
redis.call('EXPIRE', KEYS[1], ARGV[3], 'NX')
redis.call('EXPIRE', KEYS[1], ARGV[3], 'GT')
if was == '1' then return 1 end
return 0`)

func (v *Store) SetStreamLive(ctx context.Context, userID uint64, next StreamLive) (StreamLiveFold, error) {
	defer segment(ctx, "EVALSHA")()

	prior, err := foldStreamLiveScript.Exec(ctx, v.primary, []string{cache.UserKey(settingsKeyPrefix, userID)}, []string{
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
		v.client.B().Hset().
			Key(key).
			FieldValue().
			FieldValue(streamTitleField, info.Title).
			FieldValue(streamGameField, info.GameName).
			FieldValue(streamViewersField, strconv.Itoa(info.ViewerCount)).
			FieldValue(streamPeakViewersField, strconv.Itoa(info.PeakViewers)).
			FieldValue(streamStartedAtField, info.StartedAt.Format(time.RFC3339)).
			FieldValue(streamEndedAtField, info.EndedAt.Format(time.RFC3339)).
			Build(),
	)
}

func (v *Store) GetStreamInfo(ctx context.Context, userID string) (StreamInfo, bool, error) {
	defer segment(ctx, "HGET")()

	key, err := streamUserKey(userID)
	if err != nil {
		return StreamInfo{}, false, err
	}

	res := v.client.DoMulti(ctx,
		v.client.B().Hget().Key(key).Field(streamTitleField).Build(),
		v.client.B().Hget().Key(key).Field(streamGameField).Build(),
		v.client.B().Hget().Key(key).Field(streamViewersField).Build(),
		v.client.B().Hget().Key(key).Field(streamPeakViewersField).Build(),
		v.client.B().Hget().Key(key).Field(streamStartedAtField).Build(),
		v.client.B().Hget().Key(key).Field(streamEndedAtField).Build(),
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
		v.client.B().Hset().
			Key(key).
			FieldValue().
			FieldValue(streamCtrMessagesField, strconv.FormatInt(b.Messages, 10)).
			FieldValue(streamCtrAnsweredField, strconv.FormatInt(b.Answered, 10)).
			FieldValue(streamCtrModActionField, strconv.FormatInt(b.ModActions, 10)).
			Build(),
	)
}

func (v *Store) GetStreamCounterBaseline(ctx context.Context, userID string) (StreamCounters, bool, error) {
	defer segment(ctx, "HGET")()

	key, err := streamUserKey(userID)
	if err != nil {
		return StreamCounters{}, false, err
	}

	res := v.primary.DoMulti(ctx,
		v.primary.B().Hget().Key(key).Field(streamCtrMessagesField).Build(),
		v.primary.B().Hget().Key(key).Field(streamCtrAnsweredField).Build(),
		v.primary.B().Hget().Key(key).Field(streamCtrModActionField).Build(),
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
	value, err := v.primary.Do(ctx, v.primary.B().Hget().Key(key).Field(streamBaselinePendingField).Build()).AsInt64()
	if valkey.IsValkeyNil(err) {
		return false, nil
	}
	return value == version, err
}
func (v *Store) DiscardStreamBaseline(ctx context.Context, id uint64, version int64) error {
	return v.primary.Do(ctx, v.primary.B().Eval().Script(`if redis.call('HGET',KEYS[1],'stream_baseline_pending_ver')==ARGV[1] then redis.call('HDEL',KEYS[1],'stream_baseline_pending_ver') end return 1`).Numkeys(1).Key(cache.UserKey(settingsKeyPrefix, id)).Arg(strconv.FormatInt(version, 10)).Build()).Error()
}
func (v *Store) SetStreamCounterBaselineForVersion(ctx context.Context, id uint64, version int64, b StreamCounters) error {
	return v.primary.Do(ctx, v.primary.B().Eval().Script(`local confirmed=tonumber(redis.call('GET',KEYS[2]) or '0')
 if confirmed>tonumber(ARGV[1]) then return 0 end
 if redis.call('HGET',KEYS[1],'live')~='1' or redis.call('HGET',KEYS[1],'live_ver')~=ARGV[1] or redis.call('HGET',KEYS[1],'stream_baseline_pending_ver')~=ARGV[1] then return 0 end
 redis.call('HSET',KEYS[1],'streamctr:messages',ARGV[2],'streamctr:answered',ARGV[3],'streamctr:mod_actions',ARGV[4])
 redis.call('HDEL',KEYS[1],'stream_baseline_pending_ver') return 1`).Numkeys(2).Key(cache.UserKey(settingsKeyPrefix, id), livekey.VerKey(id)).Arg(strconv.FormatInt(version, 10), strconv.FormatInt(b.Messages, 10), strconv.FormatInt(b.Answered, 10), strconv.FormatInt(b.ModActions, 10)).Build()).Error()
}
