package engine

import (
	"context"
	"encoding/json"
	"testing"

	"ItsBagelBot/internal/domain/event/data"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestCounterTokenNames(t *testing.T) {
	assert.Nil(t, counterTokenNames("no tokens here {user}"))
	assert.Equal(t, []string{"deaths"}, counterTokenNames("we died {counter:deaths} times"))
	// Dedup, normalization and multiple names in first-appearance order.
	assert.Equal(t, []string{"deaths", "wins"},
		counterTokenNames("{counter:Deaths} {counter:wins} {counter:deaths}"))
	// Unclosed token contributes nothing.
	assert.Nil(t, counterTokenNames("{counter:deaths"))
	// Empty name skipped.
	assert.Nil(t, counterTokenNames("{counter:}"))
}

func TestExpandCommandCounterToken(t *testing.T) {
	out := string(expandCommand(nil, "died {counter:deaths} times", tokens{
		counters: map[string]string{"deaths": "42"},
	}))
	assert.Equal(t, "died 42 times", out)

	// No resolved value: the token stays visible, matching unknown tokens.
	out = string(expandCommand(nil, "died {counter:deaths} times", tokens{}))
	assert.Equal(t, "died {counter:deaths} times", out)
}

func TestLoyaltyConfigDefaults(t *testing.T) {
	var cfg LoyaltyModuleConfig
	assert.Equal(t, "points", cfg.Name())
	assert.Equal(t, int64(defaultSubPoints), cfg.EffectiveSubPoints())
	assert.Equal(t, int64(defaultResubPoints), cfg.EffectiveResubPoints())
	assert.Equal(t, int64(defaultGiftSubPoints), cfg.EffectiveGiftSubPoints())
	assert.Equal(t, int64(defaultCheerPointsPer100), cfg.EffectiveCheerPointsPer100())
	assert.Equal(t, int64(defaultWatchPointsPerTick), cfg.EffectiveWatchPointsPerTick())

	// Explicit value wins; negative switches the source off.
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

// rawPublisher captures published payloads verbatim, keyed by subject.
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

	// Two accruals for the same viewer fold into one entry; a big tick chunks.
	r.Earn(1, 7, "viewer7", "", 100, 300)
	r.Earn(1, 7, "", "Viewer7", 50, 0)
	for i := uint64(100); i < 100+1200; i++ {
		r.Earn(1, i, "", "", 10, 300)
	}
	r.Bump(1, "deaths", data.CounterScopeChannel, 0, "", 1)
	r.Bump(1, "deaths", data.CounterScopeChannel, 0, "", 2)
	r.Bump(1, "hugs", data.CounterScopeViewer, 7, "", 1)
	r.Bump(1, "uses", data.CounterScopeViewerCommand, 7, "hug", 4)
	r.Close() // flushes

	earned := pub.payloads[data.SubjectLoyaltyEarned]
	require.Len(t, earned, 2, "1201 entries must chunk into 2 events")
	total := 0
	var viewer7 *data.LoyaltyEarnEntry
	for _, raw := range earned {
		var dto data.LoyaltyEarnedDTO
		require.NoError(t, json.Unmarshal(raw, &dto))
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
	require.NoError(t, json.Unmarshal(bumps[0], &dto))
	require.Len(t, dto.Bumps, 3)
	byName := map[string]data.CounterBumpEntry{}
	for _, b := range dto.Bumps {
		byName[b.Name+":"+b.Scope] = b
	}
	assert.Equal(t, int64(3), byName["deaths:channel"].Delta)
	assert.Equal(t, int64(1), byName["hugs:viewer"].Delta)
	assert.Equal(t, uint64(7), byName["hugs:viewer"].ViewerID)
	assert.Equal(t, int64(4), byName["uses:viewer_command"].Delta)
	assert.Equal(t, "hug", byName["uses:viewer_command"].Command)
}

func TestLoyaltyReporterSkipsEmpty(t *testing.T) {
	pub := &rawPublisher{}
	r := NewLoyaltyReporter(pub, zap.NewNop())
	r.Earn(0, 7, "", "", 10, 0)              // no broadcaster
	r.Earn(1, 0, "", "", 10, 0)              // no viewer
	r.Earn(1, 7, "", "", 0, 0)               // nothing earned
	r.Bump(1, "", "channel", 0, "", 1)       // no name
	r.Bump(1, "deaths", "channel", 0, "", 0) // no delta
	r.Close()
	assert.Empty(t, pub.payloads)
}
