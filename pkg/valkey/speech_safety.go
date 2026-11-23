// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package valkey

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	valkey_go "github.com/valkey-io/valkey-go"
)

const speechSafetyPrefix = "tts:safety:"

type SpeechLimits struct {
	ViewerChannelStrikes int64
	ViewerChannelWindow  time.Duration
	ViewerChannelBlock   time.Duration
	ViewerGlobalStrikes  int64
	ViewerGlobalWindow   time.Duration
	ViewerGlobalBlock    time.Duration
	ChannelRefusals      int64
	ChannelWindow        time.Duration
	ChannelPause         time.Duration
	GlobalRefusals       int64
	GlobalWindow         time.Duration
	GlobalCooldown       time.Duration
}

type SpeechSafety struct {
	Limits SpeechLimits
	prefix string
}

type SpeechStrike struct {
	ChannelID   string
	ViewerID    string
	EventID     string
	Weight      int64
	ChannelOnly bool
}

type SpeechBlockScope string

const (
	SpeechBlockNone    SpeechBlockScope = ""
	SpeechBlockChannel SpeechBlockScope = "channel"
	SpeechBlockGlobal  SpeechBlockScope = "global"
)

type SpeechBlock struct {
	Scope SpeechBlockScope
	Until time.Time
}

type SpeechGate string

const (
	SpeechGateOpen           SpeechGate = ""
	SpeechGateChannelPaused  SpeechGate = "channel"
	SpeechGateGlobalPaused   SpeechGate = "global"
	SpeechGateOperatorPaused SpeechGate = "operator"
)

func NewSpeechSafety(limits SpeechLimits) SpeechSafety {
	return SpeechSafety{Limits: limits, prefix: speechSafetyPrefix}
}

const speechProbeEvery = 10

var speechStrikeScript = NewScript(`
local now = tonumber(ARGV[1])
local weight = tonumber(ARGV[2])
local function bump(counter, block, threshold, window, span)
  if threshold <= 0 then return end
  local total = redis.call('INCRBY', counter, weight)
  if redis.call('PTTL', counter) < 0 then redis.call('PEXPIRE', counter, window) end
  if total >= threshold then
    redis.call('DEL', counter)
    redis.call('SET', block, string.format('%d', now + span), 'PX', span)
  end
end
bump(KEYS[1], KEYS[3], tonumber(ARGV[3]), tonumber(ARGV[4]), tonumber(ARGV[5]))
bump(KEYS[2], KEYS[4], tonumber(ARGV[6]), tonumber(ARGV[7]), tonumber(ARGV[8]))
return 1`)

var speechRefusalScript = NewScript(`
local now = tonumber(ARGV[1])
local channelThreshold, channelWindow, channelPause = tonumber(ARGV[2]), tonumber(ARGV[3]), tonumber(ARGV[4])
local globalThreshold, globalWindow, globalCooldown = tonumber(ARGV[5]), tonumber(ARGV[6]), tonumber(ARGV[7])
local function tripped(counter, threshold, window)
  if threshold <= 0 then return false end
  local total = redis.call('INCR', counter)
  if redis.call('PTTL', counter) < 0 then redis.call('PEXPIRE', counter, window) end
  if total < threshold then return false end
  redis.call('DEL', counter)
  return true
end
local function open()
  redis.call('SET', KEYS[4], string.format('%d', now + globalCooldown), 'PX', globalCooldown + globalWindow)
  redis.call('DEL', KEYS[5])
end
if tripped(KEYS[1], channelThreshold, channelWindow) then
  redis.call('SET', KEYS[3], string.format('%d', now + channelPause), 'PX', channelPause)
end
if globalThreshold <= 0 then return 0 end
local state = redis.call('GET', KEYS[4])
if state then
  if now >= tonumber(state) then open() end
  return 0
end
if tripped(KEYS[2], globalThreshold, globalWindow) then open() end
return 0`)

type speechLimitField struct {
	env   string
	count *int64
	span  *time.Duration
}

type speechLimitGroup struct {
	threshold *int64
	spans     []*time.Duration
}

func speechLimitFields(l *SpeechLimits) []speechLimitField {
	return []speechLimitField{
		{env: "TTS_SAFETY_VIEWER_CHANNEL_STRIKES", count: &l.ViewerChannelStrikes},
		{env: "TTS_SAFETY_VIEWER_CHANNEL_WINDOW", span: &l.ViewerChannelWindow},
		{env: "TTS_SAFETY_VIEWER_CHANNEL_BLOCK", span: &l.ViewerChannelBlock},
		{env: "TTS_SAFETY_VIEWER_GLOBAL_STRIKES", count: &l.ViewerGlobalStrikes},
		{env: "TTS_SAFETY_VIEWER_GLOBAL_WINDOW", span: &l.ViewerGlobalWindow},
		{env: "TTS_SAFETY_VIEWER_GLOBAL_BLOCK", span: &l.ViewerGlobalBlock},
		{env: "TTS_SAFETY_CHANNEL_REFUSALS", count: &l.ChannelRefusals},
		{env: "TTS_SAFETY_CHANNEL_WINDOW", span: &l.ChannelWindow},
		{env: "TTS_SAFETY_CHANNEL_PAUSE", span: &l.ChannelPause},
		{env: "TTS_SAFETY_GLOBAL_REFUSALS", count: &l.GlobalRefusals},
		{env: "TTS_SAFETY_GLOBAL_WINDOW", span: &l.GlobalWindow},
		{env: "TTS_SAFETY_GLOBAL_COOLDOWN", span: &l.GlobalCooldown},
	}
}

func speechLimitGroups(l *SpeechLimits) []speechLimitGroup {
	return []speechLimitGroup{
		{&l.ViewerChannelStrikes, []*time.Duration{&l.ViewerChannelWindow, &l.ViewerChannelBlock}},
		{&l.ViewerGlobalStrikes, []*time.Duration{&l.ViewerGlobalWindow, &l.ViewerGlobalBlock}},
		{&l.ChannelRefusals, []*time.Duration{&l.ChannelWindow, &l.ChannelPause}},
		{&l.GlobalRefusals, []*time.Duration{&l.GlobalWindow, &l.GlobalCooldown}},
	}
}

func (f speechLimitField) parse(raw string) error {
	if f.count != nil {
		n, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || n < 0 {
			return fmt.Errorf("%s: want a non-negative integer", f.env)
		}
		*f.count = n
		return nil
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d < 0 {
		return fmt.Errorf("%s: want a non-negative duration", f.env)
	}
	*f.span = d
	return nil
}

func (g speechLimitGroup) valid() bool {
	if *g.threshold == 0 {
		return true
	}
	for _, span := range g.spans {
		if *span <= 0 {
			return false
		}
	}
	return true
}

func (f speechLimitField) load(get func(string) string) error {
	raw := strings.TrimSpace(get(f.env))
	if raw == "" {
		return nil
	}
	return f.parse(raw)
}

func loadSpeechLimits(limits *SpeechLimits, get func(string) string) error {
	for _, field := range speechLimitFields(limits) {
		if err := field.load(get); err != nil {
			return err
		}
	}
	return nil
}

func (l *SpeechLimits) complete() bool {
	for _, group := range speechLimitGroups(l) {
		if !group.valid() {
			return false
		}
	}
	return true
}

func ParseSpeechLimits(get func(string) string) (SpeechLimits, error) {
	var limits SpeechLimits
	if err := loadSpeechLimits(&limits, get); err != nil {
		return SpeechLimits{}, err
	}
	if !limits.complete() {
		return SpeechLimits{}, errors.New("tts safety: a threshold needs its window and duration")
	}
	return limits, nil
}

func (s SpeechSafety) key(parts ...string) string {
	return s.prefix + strings.Join(parts, ":")
}

func speechMillis(d time.Duration) string {
	return strconv.FormatInt(d.Milliseconds(), 10)
}

func speechCount(n int64) string {
	return strconv.FormatInt(n, 10)
}

func speechNow() string {
	return strconv.FormatInt(time.Now().UnixMilli(), 10)
}

func (s SpeechSafety) markerTTL(windows ...time.Duration) time.Duration {
	var longest time.Duration
	for _, window := range windows {
		longest = max(longest, window)
	}
	return longest
}

func (l SpeechLimits) strikesEnabled() bool {
	return l.ViewerChannelStrikes > 0 || l.ViewerGlobalStrikes > 0
}

func (s SpeechSafety) Strike(ctx context.Context, client valkey_go.Client, strike SpeechStrike) error {
	l := s.Limits
	if strike.Weight <= 0 || !l.strikesEnabled() {
		return nil
	}
	marker := FreshMemberSet{Key: s.key("strike", "seen", strike.ChannelID, strike.EventID)}
	fresh, err := marker.Add(ctx, client, "1", s.markerTTL(l.ViewerChannelWindow, l.ViewerGlobalWindow))
	if err != nil || !fresh {
		return err
	}
	globalStrikes := l.ViewerGlobalStrikes
	if strike.ChannelOnly {
		globalStrikes = 0
	}
	keys := []string{
		s.key("strikes", "channel", strike.ChannelID, strike.ViewerID),
		s.key("strikes", "global", strike.ViewerID),
		s.key("block", "channel", strike.ChannelID, strike.ViewerID),
		s.key("block", "global", strike.ViewerID),
	}
	args := []string{
		speechNow(), speechCount(strike.Weight),
		speechCount(l.ViewerChannelStrikes), speechMillis(l.ViewerChannelWindow), speechMillis(l.ViewerChannelBlock),
		speechCount(globalStrikes), speechMillis(l.ViewerGlobalWindow), speechMillis(l.ViewerGlobalBlock),
	}
	return speechStrikeScript.Exec(ctx, client, keys, args).Error()
}

func speechDeadline(ctx context.Context, client valkey_go.Client, key string) (time.Time, error) {
	raw, err := client.Do(ctx, client.B().Get().Key(key).Build()).ToString()
	if valkey_go.IsValkeyNil(err) {
		return time.Time{}, nil
	}
	if err != nil {
		return time.Time{}, err
	}
	millis, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return time.Time{}, err
	}
	return time.UnixMilli(millis), nil
}

func (s SpeechSafety) ViewerBlock(ctx context.Context, client valkey_go.Client, channelID, viewerID string) (SpeechBlock, error) {
	scopes := []struct {
		scope SpeechBlockScope
		key   string
	}{
		{SpeechBlockGlobal, s.key("block", "global", viewerID)},
		{SpeechBlockChannel, s.key("block", "channel", channelID, viewerID)},
	}
	for _, candidate := range scopes {
		until, err := speechDeadline(ctx, client, candidate.key)
		if err != nil {
			return SpeechBlock{}, err
		}
		if !until.IsZero() {
			return SpeechBlock{Scope: candidate.scope, Until: until}, nil
		}
	}
	return SpeechBlock{}, nil
}

func (s SpeechSafety) ClearViewer(ctx context.Context, client valkey_go.Client, channelID, viewerID string) error {
	del := client.B().Del().
		Key(s.key("block", "channel", channelID, viewerID)).
		Key(s.key("strikes", "channel", channelID, viewerID)).
		Build()
	return client.Do(ctx, del).Error()
}

func (s SpeechSafety) Refusal(ctx context.Context, client valkey_go.Client, channelID, eventID string) error {
	l := s.Limits
	if l.ChannelRefusals <= 0 && l.GlobalRefusals <= 0 {
		return nil
	}
	marker := FreshMemberSet{Key: s.key("refusal", "seen", channelID, eventID)}
	fresh, err := marker.Add(ctx, client, "1", s.markerTTL(l.ChannelWindow, l.GlobalWindow))
	if err != nil || !fresh {
		return err
	}
	keys := []string{
		s.key("refusals", "channel", channelID),
		s.key("refusals", "global"),
		s.key("pause", "channel", channelID),
		s.key("global", "state"),
		s.key("global", "probe"),
	}
	args := []string{
		speechNow(),
		speechCount(l.ChannelRefusals), speechMillis(l.ChannelWindow), speechMillis(l.ChannelPause),
		speechCount(l.GlobalRefusals), speechMillis(l.GlobalWindow), speechMillis(l.GlobalCooldown),
	}
	return speechRefusalScript.Exec(ctx, client, keys, args).Error()
}

func (s SpeechSafety) operatorFlag() VersionedFlag {
	return VersionedFlag{Flag: s.key("operator", "pause"), Version: s.key("operator", "version")}
}

func (s SpeechSafety) globalGate(ctx context.Context, client valkey_go.Client) (SpeechGate, error) {
	openUntil, err := speechDeadline(ctx, client, s.key("global", "state"))
	if err != nil || openUntil.IsZero() {
		return SpeechGateOpen, err
	}
	if time.Now().Before(openUntil) {
		return SpeechGateGlobalPaused, nil
	}
	probes, err := Incr(ctx, client, s.key("global", "probe"), s.Limits.GlobalWindow)
	if err != nil || (probes-1)%speechProbeEvery == 0 {
		return SpeechGateOpen, err
	}
	return SpeechGateGlobalPaused, nil
}

func (s SpeechSafety) Gate(ctx context.Context, client valkey_go.Client, channelID string) (SpeechGate, error) {
	paused, _, err := s.operatorFlag().Read(ctx, client)
	if err != nil {
		return SpeechGateOpen, err
	}
	if paused {
		return SpeechGateOperatorPaused, nil
	}
	gate, err := s.globalGate(ctx, client)
	if err != nil || gate != SpeechGateOpen {
		return gate, err
	}
	until, err := s.ChannelPausedUntil(ctx, client, channelID)
	if err != nil || until.IsZero() {
		return SpeechGateOpen, err
	}
	return SpeechGateChannelPaused, nil
}

func (s SpeechSafety) ChannelPausedUntil(ctx context.Context, client valkey_go.Client, channelID string) (time.Time, error) {
	return speechDeadline(ctx, client, s.key("pause", "channel", channelID))
}

func (s SpeechSafety) SetOperatorPause(ctx context.Context, client valkey_go.Client, on bool) error {
	_, err := s.operatorFlag().Set(ctx, client, on)
	return err
}
