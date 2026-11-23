// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"context"
	"sync"
	"testing"
	"time"

	"ItsBagelBot/app/twitch/sesame/engine"
	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/internal/domain/channelevents"
	"ItsBagelBot/internal/domain/event/lane"
	"ItsBagelBot/pkg/bus/bustest"
	pkg_valkey "ItsBagelBot/pkg/valkey"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

var feedAt = time.Date(2026, 11, 23, 10, 0, 0, 0, time.UTC)

type memoryBundle struct {
	total      int
	header     []byte
	recipients [][]byte
}

type memoryGifts struct {
	mu     sync.Mutex
	open   map[string]*memoryBundle
	order  []string
	parked [][]byte
}

func newMemoryGifts() *memoryGifts { return &memoryGifts{open: map[string]*memoryBundle{}} }

func (m *memoryGifts) Open(_ context.Context, _ engine.GiftSlot, header pkg_valkey.GiftHeader) (pkg_valkey.GiftOpened, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.open[header.ID]; ok {
		return pkg_valkey.GiftAlreadyOpen, nil
	}
	bundle := &memoryBundle{total: header.Total, header: header.Payload}
	for len(m.parked) > 0 && len(bundle.recipients) < bundle.total {
		bundle.recipients = append(bundle.recipients, m.parked[0])
		m.parked = m.parked[1:]
	}
	m.open[header.ID] = bundle
	m.order = append(m.order, header.ID)
	if len(bundle.recipients) >= bundle.total {
		return pkg_valkey.GiftComplete, nil
	}
	return pkg_valkey.GiftWaiting, nil
}

func (m *memoryGifts) Add(_ context.Context, _ engine.GiftSlot, recipient []byte) (pkg_valkey.GiftAdded, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, id := range m.order {
		bundle, ok := m.open[id]
		if ok && len(bundle.recipients) < bundle.total {
			bundle.recipients = append(bundle.recipients, recipient)
			return pkg_valkey.GiftAdded{BundleID: id, Complete: len(bundle.recipients) >= bundle.total}, nil
		}
	}
	m.parked = append(m.parked, recipient)
	return pkg_valkey.GiftAdded{}, nil
}

func (m *memoryGifts) Take(_ context.Context, _ engine.GiftSlot, bundleID string) (pkg_valkey.GiftTaken, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	bundle, ok := m.open[bundleID]
	if !ok {
		return pkg_valkey.GiftTaken{}, false, nil
	}
	delete(m.open, bundleID)
	return pkg_valkey.GiftTaken{Header: bundle.header, Recipients: bundle.recipients}, true, nil
}

type capturedChat struct {
	mu     sync.Mutex
	events []channelevents.Event
}

func (c *capturedChat) PublishChat(_ context.Context, event channelevents.Event) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.events = append(c.events, event)
	return nil
}

type feedFixture struct {
	t       *testing.T
	pub     *bustest.Publisher
	gifts   *memoryGifts
	chat    *capturedChat
	handler module.Module
}

func newFeedFixture(t *testing.T) *feedFixture {
	t.Helper()
	f := &feedFixture{t: t, pub: bustest.NewPublisher(), gifts: newMemoryGifts(), chat: &capturedChat{}}
	f.handler = ChannelFeed(engine.Deps{Pub: f.pub, GiftBundles: f.gifts, Log: zap.NewNop()})
	return f
}

func feedEnvelope(eventType, id, payload string) lane.Envelope {
	return lane.Envelope{Type: eventType, EventID: id, Event: []byte(payload), ReceivedAt: feedAt.Format(time.RFC3339)}
}

func (f *feedFixture) deliver(env lane.Envelope) {
	f.t.Helper()
	c := &module.Context{Env: env, BroadcasterID: 2, Log: zap.NewNop()}
	handle := f.handler.Events[env.Type]
	require.NotNil(f.t, handle, "ChannelFeed must handle %s", env.Type)
	require.NoError(f.t, handle(f.t.Context(), c, func(*module.Output) {}))
}

func (f *feedFixture) published(kind channelevents.Kind) []channelevents.Event {
	f.t.Helper()
	var events []channelevents.Event
	for _, msg := range f.pub.On(channelevents.Subject("2", kind)) {
		event, err := channelevents.Decode(msg.Payload)
		require.NoError(f.t, err)
		events = append(events, event)
	}
	return events
}

func TestChannelFeedNormalizesEventSubPayloads(t *testing.T) {
	base := channelevents.Event{ID: "evt-1", ChannelID: "2", OccurredAt: feedAt}
	cases := []struct {
		name      string
		eventType string
		payload   string
		want      channelevents.Event
	}{
		{"follow", "channel.follow", `{"user_id":"7","user_login":"fan","user_name":"Fan"}`,
			channelevents.Event{Kind: channelevents.Follow, User: &channelevents.User{ID: "7", Login: "fan", Name: "Fan"}}},
		{"new subscription", "channel.subscribe", `{"user_id":"7","user_login":"fan","user_name":"Fan","tier":"2000","is_gift":false}`,
			channelevents.Event{Kind: channelevents.Sub, Tier: "2000", User: &channelevents.User{ID: "7", Login: "fan", Name: "Fan"}}},
		{"resub with months, streak and emote", "channel.subscription.message",
			`{"user_id":"7","user_login":"fan","user_name":"Fan","tier":"1000","cumulative_months":14,"streak_months":6,"message":{"text":"Kappa hi","emotes":[{"begin":0,"end":4,"id":"25"}]}}`,
			channelevents.Event{Kind: channelevents.Resub, Tier: "1000", Months: 14, Streak: 6, User: &channelevents.User{ID: "7", Login: "fan", Name: "Fan"},
				Message: &channelevents.Message{Text: "Kappa hi", Fragments: []channelevents.Fragment{{Type: "emote", ID: "25", Text: "Kappa"}, {Type: "text", Text: " hi"}}}}},
		{"cheer", "channel.cheer", `{"user_id":"7","user_login":"fan","user_name":"Fan","is_anonymous":false,"bits":300,"message":"Cheer300 go"}`,
			channelevents.Event{Kind: channelevents.Cheer, Amount: 300, User: &channelevents.User{ID: "7", Login: "fan", Name: "Fan"}, Message: &channelevents.Message{Text: "Cheer300 go"}}},
		{"anonymous cheer hides the user", "channel.cheer", `{"user_id":"7","user_login":"fan","user_name":"Fan","is_anonymous":true,"bits":50}`,
			channelevents.Event{Kind: channelevents.Cheer, Amount: 50, Anonymous: true}},
		{"raid", "channel.raid", `{"from_broadcaster_user_id":"9","from_broadcaster_user_login":"raider","from_broadcaster_user_name":"Raider","viewers":42}`,
			channelevents.Event{Kind: channelevents.Raid, Amount: 42, User: &channelevents.User{ID: "9", Login: "raider", Name: "Raider"}}},
		{"redemption", engine.RedemptionAddType, `{"id":"r1","user_id":"7","user_login":"fan","user_name":"Fan","user_input":"hydrate","reward":{"id":"rw","title":"Water","cost":500}}`,
			channelevents.Event{Kind: channelevents.Redemption, User: &channelevents.User{ID: "7", Login: "fan", Name: "Fan"},
				Reward: &channelevents.Reward{ID: "rw", Title: "Water", Cost: 500, Input: "hydrate"}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFeedFixture(t)
			f.deliver(feedEnvelope(tc.eventType, "evt-1", tc.payload))

			want := tc.want
			want.ID, want.ChannelID, want.OccurredAt = base.ID, base.ChannelID, base.OccurredAt
			require.Equal(t, []channelevents.Event{want}, f.published(want.Kind))
		})
	}
}

func TestChannelFeedSkipsTrialAndUnidentifiedEvents(t *testing.T) {
	f := newFeedFixture(t)
	trial := feedEnvelope("channel.follow", "evt-1", `{"user_id":"7"}`)
	trial.Origin = "trial"

	f.deliver(trial)
	f.deliver(feedEnvelope("channel.follow", "", `{"user_id":"7"}`))

	require.Empty(t, f.published(channelevents.Follow))
}

const giftPayload = `{"user_id":"5","user_login":"gifter","user_name":"Gifter","total":3,"tier":"1000","cumulative_total":20,"is_anonymous":false}`

func recipientEnvelope(id, userID string) lane.Envelope {
	return feedEnvelope("channel.subscribe", id, `{"user_id":"`+userID+`","user_login":"u`+userID+`","user_name":"U`+userID+`","tier":"1000","is_gift":true}`)
}

func TestChannelFeedBundlesACommunityGiftWithItsRecipients(t *testing.T) {
	f := newFeedFixture(t)

	f.deliver(feedEnvelope("channel.subscription.gift", "gift-1", giftPayload))
	f.deliver(recipientEnvelope("sub-1", "11"))
	f.deliver(recipientEnvelope("sub-2", "12"))
	f.deliver(recipientEnvelope("sub-3", "13"))

	require.Empty(t, f.published(channelevents.Sub))
	require.Equal(t, []channelevents.Event{{
		ID: "gift-1", Kind: channelevents.CommunityGift, ChannelID: "2", OccurredAt: feedAt, Tier: "1000", Amount: 3, CumulativeTotal: 20,
		User:       &channelevents.User{ID: "5", Login: "gifter", Name: "Gifter"},
		Recipients: []channelevents.User{{ID: "11", Login: "u11", Name: "U11"}, {ID: "12", Login: "u12", Name: "U12"}, {ID: "13", Login: "u13", Name: "U13"}},
	}}, f.published(channelevents.CommunityGift))
}

func TestChannelFeedAdoptsRecipientsThatArriveBeforeTheGift(t *testing.T) {
	f := newFeedFixture(t)

	f.deliver(recipientEnvelope("sub-1", "11"))
	f.deliver(recipientEnvelope("sub-2", "12"))
	f.deliver(recipientEnvelope("sub-3", "13"))
	f.deliver(feedEnvelope("channel.subscription.gift", "gift-1", giftPayload))

	events := f.published(channelevents.CommunityGift)
	require.Len(t, events, 1)
	require.Len(t, events[0].Recipients, 3)
}

func TestChannelFeedPublishesAnIncompleteGiftAfterTheWindow(t *testing.T) {
	previous := giftFlushDelay
	giftFlushDelay = 10 * time.Millisecond
	t.Cleanup(func() { giftFlushDelay = previous })
	f := newFeedFixture(t)

	f.deliver(feedEnvelope("channel.subscription.gift", "gift-1", `{"user_id":"5","total":3,"tier":"1000","is_anonymous":true}`))
	f.deliver(recipientEnvelope("sub-1", "11"))

	require.Eventually(t, func() bool { return len(f.published(channelevents.CommunityGift)) == 1 }, 2*time.Second, 5*time.Millisecond)
	event := f.published(channelevents.CommunityGift)[0]
	require.Equal(t, []channelevents.User{{ID: "11", Login: "u11", Name: "U11"}}, event.Recipients)
	require.True(t, event.Anonymous)
	require.Nil(t, event.User)
}

func TestChannelFeedDecodesChatNotificationUpgrades(t *testing.T) {
	cases := []struct {
		name    string
		payload string
		want    channelevents.Event
	}{
		{"gifted sub upgraded", `{"chatter_user_id":"7","chatter_user_login":"fan","chatter_user_name":"Fan","notice_type":"gift_paid_upgrade","gift_paid_upgrade":{"gifter_is_anonymous":false,"gifter_user_id":"5","gifter_user_login":"gifter","gifter_user_name":"Gifter"}}`,
			channelevents.Event{User: &channelevents.User{ID: "7", Login: "fan", Name: "Fan"}, Gifter: &channelevents.User{ID: "5", Login: "gifter", Name: "Gifter"}}},
		{"anonymous gift upgraded", `{"chatter_user_id":"7","notice_type":"gift_paid_upgrade","gift_paid_upgrade":{"gifter_is_anonymous":true}}`,
			channelevents.Event{User: &channelevents.User{ID: "7"}, Anonymous: true}},
		{"prime upgraded", `{"chatter_user_id":"7","notice_type":"prime_paid_upgrade","prime_paid_upgrade":{"sub_tier":"1000"}}`,
			channelevents.Event{User: &channelevents.User{ID: "7"}, Prime: true, Tier: "1000"}},
		{"paying it forward", `{"chatter_user_id":"7","notice_type":"pay_it_forward","pay_it_forward":{"gifter_is_anonymous":false,"gifter_user_id":"5"}}`,
			channelevents.Event{User: &channelevents.User{ID: "7"}, Gifter: &channelevents.User{ID: "5"}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFeedFixture(t)
			f.deliver(feedEnvelope("channel.chat.notification", "evt-1", tc.payload))

			want := tc.want
			want.ID, want.Kind, want.ChannelID, want.OccurredAt = "evt-1", channelevents.GiftUpgrade, "2", feedAt
			require.Equal(t, []channelevents.Event{want}, f.published(channelevents.GiftUpgrade))
		})
	}
}

func TestChannelFeedLeavesNoticesWithAChannelTwinToThoseEvents(t *testing.T) {
	f := newFeedFixture(t)

	for _, kind := range []string{"sub", "resub", "sub_gift", "community_sub_gift"} {
		f.deliver(feedEnvelope("channel.chat.notification", "evt-"+kind, `{"chatter_user_id":"7","notice_type":"`+kind+`"}`))
	}

	for _, kind := range channelevents.Kinds() {
		require.Empty(t, f.published(kind), string(kind))
	}
}

func chatFeedContext(t *testing.T, premium bool) (*module.Context, *capturedChat, module.Module) {
	t.Helper()
	sink := &capturedChat{}
	c := &module.Context{
		BroadcasterID: 2, Log: zap.NewNop(),
		Env: lane.Envelope{
			Type: "channel.chat.message", MsgID: "msg-1", ReceivedAt: feedAt.Format(time.RFC3339),
			ChatterUserID: "7", ChatterUserLogin: "fan", ChatterUserName: "Fan", Text: "hello Kappa",
			Emotes: []lane.EmoteSpan{{ID: "25", Begin: 6, End: 11}},
			Badges: []lane.Badge{{SetID: "subscriber"}, {SetID: "bits"}, {SetID: "moderator"}},
		},
	}
	if premium {
		c.Regress = module.RegressPremium
	}
	return c, sink, ChannelFeedChat(engine.Deps{ChatFeed: sink})
}

func TestChannelFeedChatPublishesPremiumChannelLines(t *testing.T) {
	c, sink, chat := chatFeedContext(t, true)

	require.NoError(t, chat.Events["channel.chat.message"](t.Context(), c, func(*module.Output) {}))

	require.Equal(t, []channelevents.Event{{
		ID: "msg-1", Kind: channelevents.Chat, ChannelID: "2", OccurredAt: feedAt,
		User:    &channelevents.User{ID: "7", Login: "fan", Name: "Fan"},
		Message: &channelevents.Message{Text: "hello Kappa", Fragments: []channelevents.Fragment{{Type: "text", Text: "hello "}, {Type: "emote", ID: "25", Text: "Kappa"}}},
		Badges:  []string{"subscriber", "moderator"},
	}}, sink.events)
}

func TestChannelFeedChatSkipsStandardChannels(t *testing.T) {
	c, sink, chat := chatFeedContext(t, false)

	require.NoError(t, chat.Events["channel.chat.message"](t.Context(), c, func(*module.Output) {}))

	require.Empty(t, sink.events)
}

type fakeChannelCounts struct{ result engine.ChannelCountsResult }

func (f fakeChannelCounts) Lookup(context.Context, string) (engine.ChannelCountsResult, error) {
	return f.result, nil
}

func TestLivePublishesSessionEventsWithHelixCounts(t *testing.T) {
	pub := bustest.NewPublisher()
	counts := fakeChannelCounts{engine.ChannelCountsResult{Followers: 120, FollowersOK: true, Subs: 9, SubsOK: false}}
	live := Live(engine.Deps{Live: &fakeLive{}, Greet: &fakeGreet{}, Pub: pub, ChannelCounts: counts, Log: zap.NewNop()})
	for _, step := range []struct{ eventType, id string }{{"stream.online", "on-1"}, {"stream.offline", "off-1"}} {
		c := &module.Context{Env: feedEnvelope(step.eventType, step.id, `{}`), BroadcasterID: 2, Log: zap.NewNop()}
		require.NoError(t, live.Events[step.eventType](t.Context(), c, func(*module.Output) {}))
	}

	require.Eventually(t, func() bool { return len(pub.On(channelevents.Subject("2", channelevents.Session))) == 2 }, 2*time.Second, 5*time.Millisecond)
	states := map[channelevents.SessionState]channelevents.SessionChange{}
	for _, msg := range pub.On(channelevents.Subject("2", channelevents.Session)) {
		event, err := channelevents.Decode(msg.Payload)
		require.NoError(t, err)
		states[event.Change.State] = *event.Change
	}
	require.Equal(t, map[channelevents.SessionState]channelevents.SessionChange{
		channelevents.SessionStarted: {State: channelevents.SessionStarted, FollowerCount: 120},
		channelevents.SessionEnded:   {State: channelevents.SessionEnded, FollowerCount: 120},
	}, states)
}
