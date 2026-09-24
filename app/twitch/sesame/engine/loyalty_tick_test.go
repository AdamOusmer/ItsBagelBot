// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"errors"
	"testing"
	"time"

	"ItsBagelBot/internal/domain/rpc/manage"
	"ItsBagelBot/internal/projection"
	"github.com/nats-io/nats.go"

	"ItsBagelBot/internal/domain/event/data"
	"ItsBagelBot/internal/domain/outgress"
	"ItsBagelBot/pkg/codec"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestRearmAfterFailure(t *testing.T) {
	assert.Equal(t, watchTickQuickRetry, rearmAfterFailure(1))
	assert.Equal(t, watchTickQuickRetry, rearmAfterFailure(watchTickQuickRetries))
	assert.Equal(t, watchTickInterval, rearmAfterFailure(watchTickQuickRetries+1))
	assert.Equal(t, watchTickInterval, rearmAfterFailure(50))
}

func TestWatchTickFailureStreak(t *testing.T) {
	c := &ValkeyLoyaltyClock{log: zap.NewNop(), failures: map[uint64]int{}}

	assert.Equal(t, watchTickQuickRetry, c.settleFailure(7, errors.New("boom")))
	assert.Equal(t, watchTickQuickRetry, c.settleFailure(7, errors.New("boom")))
	assert.Equal(t, watchTickInterval, c.settleFailure(7, errors.New("boom")))
	assert.Len(t, c.failures, 1)
}

func TestWatchTickSettleSuccess(t *testing.T) {
	client := newHotPathTestClient(t)
	ctx := context.Background()
	pub := &rawPublisher{}
	clock := func() *ValkeyLoyaltyClock {
		return NewValkeyLoyaltyClock(client, nil, nil, nil, nil, LoyaltyClockConfig{
			Publisher: pub, OutgressSystemSubject: "test.system",
		})
	}
	key := loyaltyReconfirmKeyPrefix + "77001"
	require.NoError(t, client.Do(ctx, client.B().Del().Key(key).Build()).Error())
	t.Cleanup(func() { client.Do(ctx, client.B().Del().Key(key).Build()) })
	c := clock()
	c.settleFailure(77001, errors.New("boom"))
	assert.Equal(t, watchTickInterval, c.settleSuccess(ctx, 77001))
	assert.Empty(t, c.failures)
	for range 20 {
		clock().settleSuccess(ctx, 77001)
	}
	require.Len(t, pub.payloads["test.system"], 1)
	var msg outgress.Message
	require.NoError(t, codec.Unmarshal(pub.payloads["test.system"][0], &msg))
	assert.Equal(t, outgress.TypeStreamStatus, msg.Type)
	assert.Equal(t, "77001", msg.BroadcasterID)
	ttl, err := client.Do(ctx, client.B().Pttl().Key(key).Build()).AsInt64()
	require.NoError(t, err)
	assert.InDelta(t, watchTickReconfirmInterval.Milliseconds(), ttl, 5000)

	require.NoError(t, client.Do(ctx, client.B().Del().Key(key).Build()).Error())
	clock().settleSuccess(ctx, 77001)
	require.Len(t, pub.payloads["test.system"], 2)

	require.NoError(t, client.Do(ctx, client.B().Del().Key(key).Build()).Error())
	c.pub = &fakePublisher{failErr: errors.New("offline")}
	c.settleSuccess(ctx, 77001)
	clock().settleSuccess(ctx, 77001)
	require.Len(t, pub.payloads["test.system"], 3)
}

func TestWatchTickAccrual(t *testing.T) {
	for _, tc := range []struct {
		name         string
		config       string
		live         bool
		enabled      bool
		missingScope bool
		wantErr      bool
		wantPoints   int64
		wantEarn     bool
	}{
		{name: "deduplicates and excludes bot", live: true, enabled: true, wantPoints: 10, wantEarn: true},
		{name: "watch points off still tracks time", config: `{"watchPointsPerTick":-1}`, live: true, enabled: true, wantEarn: true},
		{name: "stream ended during fetch", enabled: true},
		{name: "disabled during fetch", live: true},
		{name: "bad config", config: `{"watchPointsPerTick":"bad"}`, live: true, enabled: true},
		{name: "missing scope without error text", missingScope: true, wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pub := &rawPublisher{}
			reporter := NewLoyaltyReporter(pub, zap.NewNop())
			c := NewValkeyLoyaltyClock(nil, nil, fakeReader{modules: map[string]projection.ModuleView{
				LoyaltyModuleName: {IsEnabled: tc.enabled, Configs: []byte(tc.config)},
			}}, fakeLive{live: tc.live}, reporter, LoyaltyClockConfig{BotUserID: "99"})
			c.request = func(_ context.Context, subject string, body []byte) (*nats.Msg, error) {
				assert.Equal(t, "bagel.rpc.outgress.chatters.get", subject)
				var req manage.ChattersRequest
				require.NoError(t, codec.Unmarshal(body, &req))
				assert.Equal(t, "7", req.BroadcasterID)
				body, err := codec.Marshal(manage.ChattersReply{MissingScope: tc.missingScope, Chatters: []manage.Chatter{
					{ID: "8", Login: "viewer"}, {ID: "8", Login: "viewer"}, {ID: "99", Login: "bot"}, {ID: "0"}, {ID: "bad"},
				}})
				return &nats.Msg{Data: body}, err
			}
			accrued, err := c.accrue(context.Background(), 7)
			assert.Equal(t, tc.wantEarn, accrued)
			assert.Equal(t, tc.wantErr, err != nil)
			reporter.Close()
			if !tc.wantEarn {
				assert.Empty(t, pub.payloads)
				return
			}
			require.Len(t, pub.payloads[data.SubjectLoyaltyEarned], 1)
			var dto struct {
				Entries []struct {
					Points       int64
					WatchSeconds uint64 `json:"watch_seconds"`
				}
			}
			require.NoError(t, codec.Unmarshal(pub.payloads[data.SubjectLoyaltyEarned][0], &dto))
			require.Len(t, dto.Entries, 1)
			assert.Equal(t, tc.wantPoints, dto.Entries[0].Points)
			assert.Equal(t, uint64((5 * time.Minute).Seconds()), dto.Entries[0].WatchSeconds)
		})
	}
}

func TestWatchTickFireLifecycle(t *testing.T) {
	client := newHotPathTestClient(t)
	ctx := context.Background()
	const id = uint64(77002)
	for _, scenario := range []string{"success", "fetch failed", "offline during fetch", "disabled during fetch"} {
		t.Run(scenario, func(t *testing.T) {
			keys := []string{loyaltyTickKey(id), loyaltyTickClaimPrefix + "77002"}
			require.NoError(t, client.Do(ctx, client.B().Del().Key(keys...).Build()).Error())
			t.Cleanup(func() { client.Do(ctx, client.B().Del().Key(keys...).Build()) })
			pub := &rawPublisher{}
			reporter := NewLoyaltyReporter(pub, zap.NewNop())
			proj := fakeReader{modules: map[string]projection.ModuleView{LoyaltyModuleName: {IsEnabled: true}}}
			c := NewValkeyLoyaltyClock(client, nil, proj, fakeLive{live: true}, reporter, LoyaltyClockConfig{})
			calls := 0
			c.request = func(_ context.Context, _ string, _ []byte) (*nats.Msg, error) {
				calls++
				switch scenario {
				case "fetch failed":
					return nil, errors.New("timeout")
				case "offline during fetch":
					c.live = fakeLive{live: false}
				case "disabled during fetch":
					proj.modules[LoyaltyModuleName] = projection.ModuleView{IsEnabled: false}
				}
				body, err := codec.Marshal(manage.ChattersReply{Chatters: []manage.Chatter{{ID: "8", Login: "viewer"}}})
				return &nats.Msg{Data: body}, err
			}
			c.fire(ctx, id)
			c.fire(ctx, id)
			reporter.Close()
			assert.Equal(t, 1, calls)
			ttl, err := client.Do(ctx, client.B().Ttl().Key(loyaltyTickKey(id)).Build()).AsInt64()
			require.NoError(t, err)
			switch scenario {
			case "success":
				assert.InDelta(t, watchTickInterval.Seconds(), ttl, 1)
				require.Len(t, pub.payloads[data.SubjectLoyaltyEarned], 1)
			case "fetch failed":
				assert.InDelta(t, watchTickQuickRetry.Seconds(), ttl, 1)
				assert.Empty(t, pub.payloads)
			default:
				assert.EqualValues(t, -2, ttl, "stopped streams/modules must not rearm")
				assert.Empty(t, pub.payloads)
			}
		})
	}
}
