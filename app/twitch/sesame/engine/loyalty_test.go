// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"testing"
	"time"

	"ItsBagelBot/app/twitch/sesame/engine/scope"
	"ItsBagelBot/internal/domain/event/data"
	"ItsBagelBot/internal/projection"
	"ItsBagelBot/pkg/codec"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestCounterScopePlanNames(t *testing.T) {
	assert.Empty(t, planCounters(t, "no tokens here {user}").asked)
	assert.Equal(t, []string{"deaths"}, planCounters(t, "we died {counter:deaths} times").asked)
	assert.Equal(t, []string{"deaths", "wins"},
		planCounters(t, "{counter:Deaths} {counter:wins} {counter:deaths}").asked)
	assert.Empty(t, planCounters(t, "{counter:deaths").asked)
	assert.Empty(t, planCounters(t, "{counter:}").asked)
}

func TestRenderCounterToken(t *testing.T) {
	out := renderScopes(nil, "died {counter:deaths} times", scope.Store{Peeks: &recordPeeks{}})
	assert.Equal(t, "died 42 times", out)

	out = renderScopes(nil, "died {counter:deaths} times", scope.Store{Peeks: emptyPeeks{}})
	assert.Equal(t, "died  times", out)
}

func TestLoyaltyConfigDefaults(t *testing.T) {
	var cfg LoyaltyModuleConfig
	assert.Equal(t, "points", cfg.Name())
	assert.Equal(t, int64(defaultSubPoints), cfg.EffectiveSubPoints())
	assert.Equal(t, int64(defaultResubPoints), cfg.EffectiveResubPoints())
	assert.Equal(t, int64(defaultGiftSubPoints), cfg.EffectiveGiftSubPoints())
	assert.Equal(t, int64(defaultCheerPointsPer100), cfg.EffectiveCheerPointsPer100())
	assert.Equal(t, int64(defaultWatchPointsPerTick), cfg.EffectiveWatchPointsPerTick())

	cfg = LoyaltyModuleConfig{PointsName: "bagels", SubPoints: 100, CheerPointsPer100: -1}
	assert.Equal(t, "bagels", cfg.Name())
	assert.Equal(t, int64(100), cfg.EffectiveSubPoints())
	assert.Equal(t, int64(0), cfg.EffectiveCheerPointsPer100())
}

func TestTierMultiplier(t *testing.T) {
	assert.Equal(t, int64(1), TierMultiplier("1000"))
	assert.Equal(t, int64(2), TierMultiplier("2000"))
	assert.Equal(t, int64(6), TierMultiplier("3000"))
	assert.Equal(t, int64(1), TierMultiplier(""))
	assert.Equal(t, int64(1), TierMultiplier("prime"))
}

type rawPublisher struct {
	payloads map[string][][]byte
}

func (p *rawPublisher) PublishOwned(_ context.Context, subject string, payload []byte) error {
	if p.payloads == nil {
		p.payloads = map[string][][]byte{}
	}
	p.payloads[subject] = append(p.payloads[subject], append([]byte(nil), payload...))
	return nil
}

func (p *rawPublisher) PublishOwnedWithID(ctx context.Context, subject, _ string, payload []byte) error {
	return p.PublishOwned(ctx, subject, payload)
}

func (p *rawPublisher) Flush(context.Context) error { return nil }
func (p *rawPublisher) Close() error                { return nil }

func TestLoyaltyReporterAggregatesAndChunks(t *testing.T) {
	pub := &rawPublisher{}
	r := NewLoyaltyReporter(pub, zap.NewNop())

	r.Earn(1, 7, "viewer7", "", 100, 300)
	r.Earn(1, 7, "", "Viewer7", 50, 0)
	for i := uint64(100); i < 100+1200; i++ {
		r.Earn(1, i, "", "", 10, 300)
	}
	r.Bump(ChannelBump(1, "deaths"), 1)
	r.Bump(ChannelBump(1, "deaths"), 2)
	r.Bump(CounterBumpTarget{BroadcasterID: 1, Name: "hugs", Scope: data.CounterScopeViewer, Viewer: Viewer{ID: 7, Login: "viewer7", Name: "Viewer7"}}, 1)
	r.Bump(CounterBumpTarget{BroadcasterID: 1, Name: "uses", Scope: data.CounterScopeViewerCommand, Viewer: Viewer{ID: 7}, Command: "hug"}, 4)
	r.Close()

	earned := pub.payloads[data.SubjectLoyaltyEarned]
	require.Len(t, earned, 2, "1201 entries must chunk into 2 events")
	total := 0
	var viewer7 *data.LoyaltyEarnEntry
	for _, raw := range earned {
		var dto data.LoyaltyEarnedDTO
		require.NoError(t, codec.Unmarshal(raw, &dto))
		assert.Equal(t, uint64(1), dto.UserID)
		assert.LessOrEqual(t, len(dto.Entries), loyaltyChunk)
		total += len(dto.Entries)
		for i := range dto.Entries {
			if dto.Entries[i].ViewerID == 7 {
				viewer7 = &dto.Entries[i]
			}
		}
	}
	assert.Equal(t, 1201, total)
	require.NotNil(t, viewer7)
	assert.Equal(t, int64(150), viewer7.Points)
	assert.Equal(t, uint64(300), viewer7.WatchSeconds)
	assert.Equal(t, "viewer7", viewer7.ViewerLogin)
	assert.Equal(t, "Viewer7", viewer7.ViewerName)

	bumps := pub.payloads[data.SubjectLoyaltyCounters]
	require.Len(t, bumps, 1)
	var dto data.CounterBumpedDTO
	require.NoError(t, codec.Unmarshal(bumps[0], &dto))
	require.Len(t, dto.Bumps, 3)
	byName := map[string]data.CounterBumpEntry{}
	for _, b := range dto.Bumps {
		byName[b.Name+":"+b.Scope] = b
	}
	assert.Equal(t, int64(3), byName["deaths:channel"].Delta)
	assert.Equal(t, int64(1), byName["hugs:viewer"].Delta)
	assert.Equal(t, uint64(7), byName["hugs:viewer"].ViewerID)
	assert.Equal(t, "viewer7", byName["hugs:viewer"].ViewerLogin)
	assert.Equal(t, "Viewer7", byName["hugs:viewer"].ViewerName)
	assert.Equal(t, int64(4), byName["uses:viewer_command"].Delta)
	assert.Equal(t, "hug", byName["uses:viewer_command"].Command)
}

func TestLoyaltyReporterSkipsEmpty(t *testing.T) {
	pub := &rawPublisher{}
	r := NewLoyaltyReporter(pub, zap.NewNop())
	r.Earn(0, 7, "", "", 10, 0)
	r.Earn(1, 0, "", "", 10, 0)
	r.Earn(1, 7, "", "", 0, 0)
	r.Bump(ChannelBump(1, ""), 1)
	r.Bump(ChannelBump(1, "deaths"), 0)
	r.Bump(ChannelBump(0, "deaths"), 1)
	r.Bump(CounterBumpTarget{BroadcasterID: 1, Name: "feeds", Scope: data.CounterScopeBot}, 1)
	r.Close()
	assert.Empty(t, pub.payloads)
}

func TestLoyaltyReporterBotNamespace(t *testing.T) {
	pub := &rawPublisher{}
	r := NewLoyaltyReporter(pub, zap.NewNop())
	r.Bump(BotBump("feeds"), 2)
	r.Close()

	bumps := pub.payloads[data.SubjectLoyaltyCounters]
	require.Len(t, bumps, 1)
	var dto data.CounterBumpedDTO
	require.NoError(t, codec.Unmarshal(bumps[0], &dto))
	assert.Equal(t, uint64(0), dto.UserID)
	require.Len(t, dto.Bumps, 1)
	assert.Equal(t, data.CounterScopeBot, dto.Bumps[0].Scope)
	assert.Equal(t, int64(2), dto.Bumps[0].Delta)
}

func TestBumpTargetRouting(t *testing.T) {
	scope, viewer, cmd := bumpTarget(data.CounterScopeCommand, 7, "raid")
	assert.Equal(t, data.CounterScopeCommand, scope)
	assert.Equal(t, uint64(0), viewer)
	assert.Equal(t, "raid", cmd)

	scope, viewer, cmd = bumpTarget(data.CounterScopeViewer, 0, "raid")
	assert.Equal(t, data.CounterScopeChannel, scope)
	assert.Equal(t, uint64(0), viewer)
	assert.Empty(t, cmd)

	scope, _, _ = bumpTarget(data.CounterScopeBot, 7, "raid")
	assert.Equal(t, data.CounterScopeBot, scope)

	assert.Equal(t, "raid:0", entryField(data.CounterScopeCommand, 0, "raid"))
	assert.Equal(t, "raid:7", entryField(data.CounterScopeViewerCommand, 7, "raid"))
	assert.Equal(t, "7", entryField(data.CounterScopeViewer, 7, ""))
}

func TestCounterBumpRefusesSystemCounters(t *testing.T) {
	s := &ValkeyLoyaltyStore{}
	for _, name := range data.SystemCounterNames() {
		_, err := s.CounterBump(context.Background(), CounterBump{BroadcasterID: 1, Name: name, Delta: 1})
		assert.ErrorIs(t, err, ErrReservedCounter, name)
	}
	_, err := s.CounterBump(context.Background(), CounterBump{BroadcasterID: 1, Name: " !Commands_Answered ", Delta: 1})
	assert.ErrorIs(t, err, ErrReservedCounter, "normalization must not open a way around the guard")
}

func TestLoyaltyConfigRejectsMalformedRates(t *testing.T) {
	for _, raw := range []string{`{"watchPointsPerTick":"off"}`, `{"subPoints":`, `[]`} {
		cfg, enabled := ReadLoyaltyConfig(context.Background(), fakeReader{modules: map[string]projection.ModuleView{
			LoyaltyModuleName: {IsEnabled: true, Configs: []byte(raw)},
		}}, 7)
		assert.False(t, enabled, raw)
		assert.Equal(t, LoyaltyModuleConfig{}, cfg)
	}
}

type blockedLoyaltyPublisher struct {
	rawPublisher
	started chan struct{}
	release chan struct{}
}

func (p *blockedLoyaltyPublisher) PublishOwned(ctx context.Context, subject string, body []byte) error {
	if len(p.payloads) == 0 {
		close(p.started)
		<-p.release
	}
	return p.rawPublisher.PublishOwned(ctx, subject, body)
}

func TestLoyaltyReporterCloseWaitsForPublish(t *testing.T) {
	pub := &blockedLoyaltyPublisher{started: make(chan struct{}), release: make(chan struct{})}
	r := NewLoyaltyReporter(pub, zap.NewNop())
	r.Earn(1, 7, "viewer", "", 10, 300)
	r.nudge()
	<-pub.started
	closed := make(chan struct{})
	go func() { r.Close(); close(closed) }()
	select {
	case <-closed:
		t.Error("Close returned before its in-flight publish completed")
	case <-time.After(20 * time.Millisecond):
	}
	close(pub.release)
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("Close did not finish after publishing")
	}
	require.Len(t, pub.payloads[data.SubjectLoyaltyEarned], 1)
}
