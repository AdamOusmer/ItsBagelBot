// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package projection

// Per-stream projection: the stream:* metadata fields, the streamctr:*
// counter baselines and the live flag. Split from valkey.go so the stream
// section reads as one file beside the command, module and fetch sections
// (the valkey_fetch.go precedent). Every field here is a LIVE FIELD on the
// shared settings:<user_id> hash, never a marked collection section.

import (
	"context"
	"strconv"
	"time"

	"ItsBagelBot/internal/utils"
	"ItsBagelBot/pkg/cache"

	"github.com/valkey-io/valkey-go"
)

// Per-stream metadata and counter-baseline fields live in the same
// settings:<user_id> hash as every other section (LIVE FIELD PRECEDENT:
// see SetStreamLive/GetStreamLive below). streamTitleField doubles as the presence
// anchor for GetStreamInfo's known check; streamCtrMessagesField is the same
// anchor for GetStreamCounterBaseline.
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
)

// streamUserKey parses the RPC-carried string broadcaster id (the shape
// StreamInfoRequest/StreamCounters callers already hold, straight off the
// wire) into the uint64 cache.UserKey expects. The stream-info and
// counter-baseline accessors take a string id instead of matching the rest
// of Store's uint64 convention, so this is the one place that parse happens.
func streamUserKey(userID string) (string, error) {
	id, err := strconv.ParseUint(userID, 10, 64)
	if err != nil {
		return "", err
	}
	return cache.UserKey(settingsKeyPrefix, id), nil
}

// GetStreamLive reads the projected live/offline signal for one user. known is
// false when the field is absent (the projector has not seen a stream event and
// the hash has no live entry), letting the caller escalate instead of assuming
// offline.
func (v *Store) GetStreamLive(ctx context.Context, userID uint64) (live bool, known bool, err error) {
	defer segment(ctx, "HGET")()

	key := cache.UserKey(settingsKeyPrefix, userID)

	res, err := v.client.Do(ctx, v.client.B().Hget().Key(key).Field("live").Build()).ToString()
	if err != nil {
		if valkey.IsValkeyNil(err) {
			return false, false, nil
		}
		return false, false, err
	}
	return res == "1", true, nil
}

// SetStreamLive projects Twitch's current live/offline signal for one user.
func (v *Store) SetStreamLive(ctx context.Context, userID uint64, live bool) error {

	defer segment(ctx, "HSET")()

	key := cache.UserKey(settingsKeyPrefix, userID)

	return v.pipelineWithTTL(ctx, key, DefaultTTL,
		v.client.B().Hset().
			Key(key).
			FieldValue().
			FieldValue("live", utils.BoolField(live)).
			Build(),
	)
}

// StreamInfo is the projected per-stream metadata shown on the Overview
// dashboard: title/game as Twitch reports them, the current and peak viewer
// counts, and the stream's start/end timestamps. It rides the same
// settings:<user_id> hash as every other LIVE FIELD (no :projected marker:
// see the sectionWrite comment and clearProjectionFields in valkey.go for
// why markers are reserved for full-section collection writes).
type StreamInfo struct {
	Title       string
	GameName    string
	ViewerCount int
	PeakViewers int
	StartedAt   time.Time
	EndedAt     time.Time
}

// SetStreamInfo projects Twitch's current stream metadata for one user.
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

// GetStreamInfo reads the projected stream metadata for one user. known is
// false when streamTitleField is absent (mirrors GetStreamLive's "live"
// check), letting the caller escalate instead of assuming an empty/offline
// stream. The six fields are fetched as individual HGETs in one DoMulti round
// trip (the GetCommand pattern) rather than one HMGET, because HMGET collapses
// a missing field and a genuinely empty one to the same "" and known needs to
// tell those apart.
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

// StreamCounters is a snapshot of the counters an Overview panel diffs
// against to show per-stream deltas (this stream's messages/answers/mod
// actions, not the lifetime totals the fleet log already tracks).
type StreamCounters struct {
	Messages   int64
	Answered   int64
	ModActions int64
}

// SetStreamCounterBaseline projects the counter values observed at the start
// of the current stream, so a later read can subtract this snapshot to get a
// per-stream delta.
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

// GetStreamCounterBaseline reads the counter snapshot taken at stream start.
// known is false when streamCtrMessagesField is absent.
//
// This read is pinned to v.primary (pkg/valkey/routing.go's Primary wrapper):
// a dashboard opened right as a stream starts can race the baseline write
// against the node-local replica that plain Do would hit, so an
// under-replicated read would show a wrong (non-zero) delta for the first
// few seconds of every stream. Primary trades that staleness window for a
// Sentinel round trip on this one read.
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
