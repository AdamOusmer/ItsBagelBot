// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	"ItsBagelBot/app/twitch/sesame/engine"
	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/internal/domain/event/data"
	"ItsBagelBot/internal/domain/event/lane"
	"ItsBagelBot/internal/domain/i18n"
	loyaltyrpc "ItsBagelBot/internal/domain/rpc/loyalty"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type chatter struct {
	id     string
	badges []lane.Badge
}

var (
	viewer    = chatter{id: "7"}
	streamer  = chatter{id: "2"}
	moderator = chatter{id: "9", badges: []lane.Badge{{SetID: "moderator"}}}
)

func loyaltyCtx(in eventInput) *module.Context {
	c := eventCtx(in)
	c.Env.BroadcasterUserID = "2"
	c.Env.ChatterUserID = "7"
	c.Env.ChatterUserLogin = "coolviewer"
	return c
}

func loyaltyChat(who chatter, config string) *module.Context {
	c := loyaltyCtx(eventInput{"channel.chat.message", "", config})
	c.Env.ChatterUserID = who.id
	c.Env.Badges = who.badges
	return c
}

func loyaltyDeps(fake *fakeLoyalty) engine.Deps {
	lookup := &fakeAccountAge{result: engine.AccountAgeResult{TargetID: "8", UserFound: true}}
	return engine.Deps{Loyalty: fake, TwitchAccounts: lookup, Log: zap.NewNop()}
}

func runLoyaltyChat(t *testing.T, fake *fakeLoyalty, c *module.Context, text string) []module.Output {
	t.Helper()
	return runChat(t, Loyalty(loyaltyDeps(fake)), c, text)
}

const loyaltySubJSON = `{"user_id":"7","user_login":"coolviewer","user_name":"CoolViewer","tier":"1000"}`

func TestLoyaltyAccrual(t *testing.T) {
	cases := []struct {
		name    string
		event   string
		payload string
		config  string
		points  int64
	}{
		{"a sub earns the default points", "channel.subscribe", loyaltySubJSON, "", 500},
		{"a tier three sub earns the tier multiple", "channel.subscribe", `{"user_id":"7","user_login":"coolviewer","user_name":"CoolViewer","tier":"3000"}`, "", 3000},
		{"a sub earns nothing when the source is disabled", "channel.subscribe", loyaltySubJSON, `{"subPoints":-1}`, 0},
		{"a gifter earns for every gifted sub", "channel.subscription.gift", `{"is_anonymous":false,"user_id":"7","user_login":"coolviewer","user_name":"CoolViewer","total":5,"tier":"1000"}`, "", 500},
		{"an anonymous gifter earns nothing", "channel.subscription.gift", `{"is_anonymous":true,"total":5,"tier":"1000"}`, "", 0},
		{"bits are pro rated", "channel.cheer", `{"is_anonymous":false,"user_id":"7","user_login":"coolviewer","user_name":"CoolViewer","bits":250}`, "", 125},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake := &fakeLoyalty{}
			out := runEvent(t, Loyalty(loyaltyDeps(fake)), loyaltyCtx(eventInput{tc.event, tc.payload, tc.config}))
			assert.Empty(t, out, "accrual must be silent")
			var want []earnCall
			if tc.points > 0 {
				want = []earnCall{{2, 7, "coolviewer", "CoolViewer", tc.points, 0}}
			}
			assert.Equal(t, want, fake.earns)
		})
	}
}

func TestLoyaltyEventStreamerPointsPreference(t *testing.T) {
	for _, event := range []struct {
		name   string
		extra  string
		points int64
	}{
		{"channel.subscribe", `,"tier":"1000"`, 500},
		{"channel.subscription.message", `,"tier":"2000"`, 1000},
		{"channel.subscription.gift", `,"total":3`, 300},
		{"channel.cheer", `,"bits":200`, 100},
	} {
		for _, tc := range []struct {
			name   string
			viewer string
			config string
			earns  bool
		}{
			{"streamer default", "2", "", true},
			{"streamer off", "2", `{"streamerPoints":-1}`, false},
			{"viewer while streamer off", "7", `{"streamerPoints":-1}`, true},
		} {
			t.Run(event.name+"/"+tc.name, func(t *testing.T) {
				fake := &fakeLoyalty{}
				payload := fmt.Sprintf(`{"user_id":"%s","user_login":"person"%s}`, tc.viewer, event.extra)
				runEvent(t, Loyalty(loyaltyDeps(fake)), loyaltyCtx(eventInput{event.name, payload, tc.config}))
				if !tc.earns {
					require.Empty(t, fake.earns)
					return
				}
				require.Len(t, fake.earns, 1)
				require.Equal(t, event.points, fake.earns[0].points)
			})
		}
	}
}

func TestLoyaltyRejectsMalformedConfig(t *testing.T) {
	fake := &fakeLoyalty{}
	m := Loyalty(loyaltyDeps(fake))
	var col collector
	c := loyaltyCtx(eventInput{"channel.subscribe", loyaltySubJSON, `{"subPoints":"bad"}`})
	require.Error(t, m.Events["channel.subscribe"](context.Background(), c, col.emit))
	assert.Empty(t, fake.earns)

	_, err := runChatErr(t, m, loyaltyChat(viewer, `{"viewerTransfers":"bad"}`), "!points give @receiver 100")
	require.Error(t, err)
	assert.Empty(t, fake.transfers)
}

type loyaltyClaims map[string]bool

func (s loyaltyClaims) Seen(_ context.Context, key string, _ time.Duration) (bool, error) {
	seen := s[key]
	s[key] = true
	return seen, nil
}

func (s loyaltyClaims) Release(_ context.Context, key string) error {
	delete(s, key)
	return nil
}

func TestLoyaltyAwardsDeduplicateRedelivery(t *testing.T) {
	for _, tc := range []struct{ event, body string }{
		{"channel.subscribe", loyaltySubJSON},
		{"channel.subscription.message", loyaltySubJSON},
		{"channel.subscription.gift", `{"user_id":"7","total":5}`},
		{"channel.cheer", `{"user_id":"7","bits":100}`},
	} {
		t.Run(tc.event, func(t *testing.T) {
			fake := &fakeLoyalty{}
			claims := loyaltyClaims{}
			c := loyaltyCtx(eventInput{tc.event, tc.body, ""})
			c.Env.EventID = "event-one"
			var col collector
			for range 2 {
				m := Loyalty(engine.Deps{Loyalty: fake, Dedup: engine.NewEventDedup(claims, "", time.Hour, nil)})
				require.NoError(t, m.Events[tc.event](context.Background(), c, col.emit))
			}
			require.Len(t, fake.earns, 1, "a redelivery on another replica must not award again")

			c.Env.EventID = "event-two"
			m := Loyalty(engine.Deps{Loyalty: fake, Dedup: engine.NewEventDedup(claims, "", time.Hour, nil)})
			require.NoError(t, m.Events[tc.event](context.Background(), c, col.emit))
			require.Len(t, fake.earns, 2, "a distinct event must still earn")
		})
	}
}

func TestLoyaltyPointMutationsDeduplicateRedelivery(t *testing.T) {
	for _, verb := range []string{"add", "remove", "set", "give"} {
		t.Run(verb, func(t *testing.T) {
			fake := &fakeLoyalty{}
			claims := loyaltyClaims{}
			c := loyaltyChat(streamer, "")
			c.Env.MsgID = "chat-one"
			for range 2 {
				d := loyaltyDeps(fake)
				d.Dedup = engine.NewEventDedup(claims, "", time.Hour, nil)
				runChat(t, Loyalty(d), c, "!points "+verb+" @receiver 100")
			}
			assert.Equal(t, 1, len(fake.adjusts)+len(fake.transfers))
		})
	}
}

type versionedWatchTicker struct {
	version   int64
	armed     bool
	completed chan bool
}

func (f *versionedWatchTicker) Arm(context.Context, uint64) {
	panic("versioned ticker called through legacy Arm")
}

func (f *versionedWatchTicker) Disarm(context.Context, uint64) {
	panic("versioned ticker called through legacy Disarm")
}

func (f *versionedWatchTicker) ArmVersioned(_ context.Context, _ uint64, version int64) {
	if version >= f.version {
		f.version = version
		f.armed = true
	}
	f.completed <- f.armed
}

func (f *versionedWatchTicker) DisarmVersioned(_ context.Context, _ uint64, version int64) {
	if version >= f.version {
		f.version = version
		f.armed = false
	}
	f.completed <- f.armed
}

func TestLoyaltyLifecycleForwardsVersionAndRejectsStaleOffline(t *testing.T) {
	tick := &versionedWatchTicker{completed: make(chan bool, 1)}
	m := Loyalty(engine.Deps{LoyaltyTick: tick, Log: zap.NewNop()})
	events := []struct {
		event, at string
		wantArmed bool
	}{
		{"stream.online", "2026-09-25T20:01:00Z", true},
		{"stream.offline", "2026-09-25T20:00:00Z", true},
		{"stream.offline", "2026-09-25T20:02:00Z", false},
		{"stream.online", "2026-09-25T20:01:00Z", false},
	}
	for _, ev := range events {
		require.NoError(t, m.Events[ev.event](context.Background(), liveCtx(ev.event, ev.at), func(*module.Output) {}))
		select {
		case armed := <-tick.completed:
			require.Equal(t, ev.wantArmed, armed, ev.event+" "+ev.at)
		case <-time.After(time.Second):
			t.Fatal("lifecycle task did not finish")
		}
	}
}

type watchtimeLoyalty struct {
	fakeLoyalty
	seconds                 uint64
	err                     error
	broadcasterID, viewerID uint64
}

func (f *watchtimeLoyalty) BalanceGet(_ context.Context, broadcasterID, viewerID uint64) (loyaltyrpc.Balance, error) {
	f.broadcasterID, f.viewerID = broadcasterID, viewerID
	return loyaltyrpc.Balance{WatchSeconds: f.seconds}, f.err
}

func TestLoyaltyWatchtimeCommand(t *testing.T) {
	for _, tc := range []struct {
		name, locale, want string
		seconds            uint64
		err                error
	}{
		{name: "recorded time", locale: "en", seconds: 9000, want: "@coolviewer you have watched for 2 hours, 30 minutes."},
		{name: "no recorded time", locale: "en", want: "@coolviewer you have watched for less than a minute."},
		{name: "duration overflow", locale: "en", seconds: ^uint64(0), want: "@coolviewer you have watched for 292 years, 5 months."},
		{name: "localized time", locale: "fr", seconds: 7200, want: "@coolviewer vous avez regardé pendant 2 heures."},
		{name: "store unavailable", locale: "en", err: errors.New("loyalty unavailable"), want: "@coolviewer that didn't work, please try again."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fake := &watchtimeLoyalty{seconds: tc.seconds, err: tc.err}
			m := Loyalty(engine.Deps{Loyalty: fake, Log: zap.NewNop()})
			cmd := findCmd(t, m, "watchtime")
			assert.Equal(t, module.RoleEveryone, cmd.Perm)
			assert.Equal(t, 5*time.Second, cmd.Cooldown)
			c := loyaltyChat(viewer, "")
			c.Locale = tc.locale
			assert.Equal(t, []string{tc.want}, texts(runChat(t, m, c, "!watchtime")))
			assert.Equal(t, uint64(2), fake.broadcasterID)
			assert.Equal(t, uint64(7), fake.viewerID)
			assert.Empty(t, fake.earns)
		})
	}
}

func TestPointsCommand(t *testing.T) {
	cases := []struct {
		name      string
		text      string
		as        chatter
		config    string
		bad       bool
		contains  []string
		transfers []transferCall
		adjusts   []adjustCall
	}{
		{name: "shows the balance with the configured currency name", text: "!points", as: viewer, config: `{"pointsName":"bagels"}`,
			contains: []string{"1234", "bagels", "2.0"}},
		{name: "give transfers the chatter's own points", text: "!points give @bagelfan 500", as: viewer,
			transfers: []transferCall{{7, 8, "bagelfan", 500}}, contains: []string{"bagelfan", "500"}},
		{name: "give is refused while viewer transfers are off", text: "!points give bagelfan 100", as: viewer,
			config: `{"viewerTransfers":-1}`, contains: []string{"turned off"}},
		{name: "give stays allowed when viewer transfers are zero", text: "!points give bagelfan 100", as: viewer,
			config: `{"viewerTransfers":0}`, transfers: []transferCall{{7, 8, "bagelfan", 100}}, contains: []string{"bagelfan"}},
		{name: "give to yourself is refused", text: "!points give @CoolViewer 10", as: viewer, contains: []string{"yourself"}},
		{name: "give above the balance reports the standing", text: "!points give bagelfan 999999", as: viewer, bad: true,
			transfers: []transferCall{{7, 8, "bagelfan", 999999}}, contains: []string{"1234"}},
		{name: "give to an unknown viewer asks to retry", text: "!points give ghost 10", as: viewer,
			transfers: []transferCall{{7, 8, "ghost", 10}}, contains: []string{"try again"}},
		{name: "give with a non numeric amount prints usage", text: "!points give bagelfan nope", as: viewer, contains: []string{"usage"}},
		{name: "set grants an absolute balance", text: "!points set @CoolViewer 500", as: streamer,
			adjusts: []adjustCall{{8, "coolviewer", 500, true}}, contains: []string{"500", "coolviewer"}},
		{name: "add applies a negative delta", text: "!points add coolviewer -100", as: streamer,
			adjusts: []adjustCall{{8, "coolviewer", -100, false}}, contains: []string{"1134"}},
		{name: "remove subtracts", text: "!points remove @CoolViewer 100", as: streamer,
			adjusts: []adjustCall{{8, "coolviewer", -100, false}}, contains: []string{"1134"}},
		{name: "set on an unknown viewer asks to retry", text: "!points set ghost 10", as: streamer,
			adjusts: []adjustCall{{8, "ghost", 10, true}}, contains: []string{"try again"}},
		{name: "a plain viewer never reaches the adjust path", text: "!points set @CoolViewer 500", as: viewer, contains: []string{"1234"}},
		{name: "mods cannot set points while modSetPoints is off", text: "!points set coolviewer 50", as: moderator,
			config: `{"modSetPoints":-1}`, contains: []string{"turned off"}},
		{name: "mods may still add points while modSetPoints is off", text: "!points add coolviewer 50", as: moderator,
			config: `{"modSetPoints":-1}`, adjusts: []adjustCall{{8, "coolviewer", 50, false}}, contains: []string{"1284"}},
		{name: "mods cannot add points while modAdjustPoints is off", text: "!points add coolviewer 50", as: moderator,
			config: `{"modAdjustPoints":-1}`, contains: []string{"turned off"}},
		{name: "the streamer bypasses the mod toggles", text: "!points set coolviewer 50", as: streamer,
			config: `{"modSetPoints":-1}`, adjusts: []adjustCall{{8, "coolviewer", 50, true}}, contains: []string{"50"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake := &fakeLoyalty{transferBad: tc.bad}
			out := runLoyaltyChat(t, fake, loyaltyChat(tc.as, tc.config), tc.text)
			require.Len(t, out, 1)
			for _, want := range tc.contains {
				assert.Contains(t, strings.ToLower(out[0].Text), want)
			}
			assert.Equal(t, tc.transfers, fake.transfers)
			assert.Equal(t, tc.adjusts, fake.adjusts)
		})
	}
}

func TestPointsGiveRecipientLookup(t *testing.T) {
	for _, tc := range []struct {
		name         string
		lookup       *fakeAccountAge
		wantText     string
		wantTransfer bool
	}{
		{"new recipient", &fakeAccountAge{result: engine.AccountAgeResult{UserFound: true, TargetID: "42"}}, "you gave", true},
		{"unknown account", &fakeAccountAge{}, "was not found", false},
		{"lookup failure", &fakeAccountAge{err: errors.New("unavailable")}, "try again", false},
		{"missing lookup", nil, "try again", false},
		{"invalid lookup ID", &fakeAccountAge{result: engine.AccountAgeResult{UserFound: true, TargetID: "0"}}, "try again", false},
		{"self under another login", &fakeAccountAge{result: engine.AccountAgeResult{UserFound: true, TargetID: "7"}}, "yourself", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fake := &fakeLoyalty{}
			deps := engine.Deps{Loyalty: fake, Log: zap.NewNop()}
			if tc.lookup != nil {
				deps.TwitchAccounts = tc.lookup
			}
			out := runChat(t, Loyalty(deps), loyaltyChat(viewer, ""), "!points give @Blemmyz 500")
			require.Len(t, out, 1)
			assert.Contains(t, strings.ToLower(out[0].Text), tc.wantText)
			if tc.wantTransfer {
				assert.Equal(t, []transferCall{{fromID: 7, targetID: 42, login: "blemmyz", amount: 500}}, fake.transfers)
			} else {
				assert.Empty(t, fake.transfers)
			}
			if tc.lookup != nil {
				assert.Equal(t, "blemmyz", tc.lookup.got.targetLogin)
			}
		})
	}
}

func TestPointsAdjustCreatesResolvedUsers(t *testing.T) {
	for _, verb := range []string{"set", "add", "remove"} {
		t.Run(verb, func(t *testing.T) {
			fake := &fakeLoyalty{}
			lookup := &fakeAccountAge{result: engine.AccountAgeResult{TargetID: "42", UserFound: true}}
			m := Loyalty(engine.Deps{Loyalty: fake, TwitchAccounts: lookup, Log: zap.NewNop()})
			runChat(t, m, loyaltyChat(streamer, ""), "!points "+verb+" @Blemmyz 5000")
			value := int64(5000)
			if verb == "remove" {
				value = -value
			}
			assert.Equal(t, []adjustCall{{viewerID: 42, login: "blemmyz", value: value, absolute: verb == "set"}}, fake.adjusts)
			assert.Equal(t, "blemmyz", lookup.got.targetLogin)
		})
	}
}

func TestPointsSetAcceptsExactBIGINTAmount(t *testing.T) {
	for _, tc := range []struct {
		name, amount, want string
		adjusts            []adjustCall
	}{
		{"largest signed amount", "9223372036854775807", "9223372036854775807", []adjustCall{{8, "blemmyz", math.MaxInt64, true}}},
		{"one past the largest amount", "9223372036854775808", "usage", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fake := &fakeLoyalty{}
			out := runLoyaltyChat(t, fake, loyaltyChat(streamer, ""), "!points set blemmyz "+tc.amount)
			require.Len(t, out, 1)
			assert.Contains(t, strings.ToLower(out[0].Text), tc.want)
			assert.Equal(t, tc.adjusts, fake.adjusts)
		})
	}
}

func TestLeaderboard(t *testing.T) {
	standings := []topViewer{
		{id: "8", login: "alpha", name: "Alpha", points: 9000},
		{id: "9", login: "beta", name: "Beta", points: 800},
		{id: "10", login: "gamma", name: "", points: 7},
	}
	cases := []struct {
		name     string
		text     string
		top      []topViewer
		contains []string
		excludes []string
	}{
		{"shows the top standings up to the limit", "!leaderboard 2", standings, []string{"1. alpha 9000", "2. beta 800"}, []string{"gamma"}},
		{"reports an empty board", "!leaderboard", nil, []string{"no standings"}, nil},
		{"rejects a limit above the maximum", "!leaderboard 99", standings[:1], []string{"usage"}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := runLoyaltyChat(t, &fakeLoyalty{topViewers: tc.top}, loyaltyChat(viewer, ""), tc.text)
			require.Len(t, out, 1)
			text := strings.ToLower(out[0].Text)
			for _, want := range tc.contains {
				assert.Contains(t, text, want)
			}
			for _, bad := range tc.excludes {
				assert.NotContains(t, text, bad)
			}
		})
	}
}

func TestCounterCommand(t *testing.T) {
	cases := []struct {
		name     string
		args     string
		contains string
		bumps    []bumpCall
		sets     int
		deleted  []string
	}{
		{name: "no arguments prints usage", args: "", contains: "usage"},
		{name: "a bare name shows the value", args: "deaths", contains: "42"},
		{name: "add bumps by the amount", args: "add deaths 3", contains: "3", bumps: []bumpCall{{2, "deaths", 7, "", 3}}},
		{name: "set writes the value", args: "set deaths 10", contains: "10", sets: 1},
		{name: "reset reports the reset", args: "reset deaths", contains: "reset", sets: 1},
		{name: "delete removes the counter", args: "delete deaths", contains: "deleted", deleted: []string{"deaths"}},
		{name: "list names the counters", args: "list", contains: "deaths"},
		{name: "an unknown counter is not found", args: "nosuch", contains: "not found"},
		{name: "create defaults to the channel scope", args: "create wins", contains: "channel"},
		{name: "create accepts a per user scope", args: "create wins user", contains: "per user"},
		{name: "create accepts a per user and command scope", args: "create hugs user+command", contains: "per user+command"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake := &fakeLoyalty{
				counters: map[string]loyaltyrpc.Counter{"deaths": {Name: "deaths", Scope: data.CounterScopeChannel, Value: 42}},
				setFound: true,
			}
			out := runLoyaltyChat(t, fake, loyaltyChat(viewer, ""), strings.TrimSpace("!counter "+tc.args))
			require.Len(t, out, 1)
			assert.Contains(t, strings.ToLower(out[0].Text), tc.contains)
			assert.Equal(t, tc.bumps, fake.bumps)
			assert.Equal(t, tc.sets, fake.setCalls)
			assert.Equal(t, tc.deleted, fake.deleted)
		})
	}
}

type variableLoyaltyBalance struct {
	engine.LoyaltyStore
	balance loyaltyrpc.Balance
	reads   int
}

func (s *variableLoyaltyBalance) BalanceGet(context.Context, uint64, uint64) (loyaltyrpc.Balance, error) {
	s.reads++
	return s.balance, nil
}

func TestLoyaltyVariablesMatchNativeRepliesAtBIGINTLimits(t *testing.T) {
	for _, tc := range []struct {
		name, locale, wantPoints string
		points                   int64
		seconds                  uint64
	}{
		{name: "above float precision", locale: "en", points: 9007199254740993, seconds: 7200, wantPoints: "9007199254740993"},
		{name: "signed BIGINT maximum", locale: "en", points: 1<<63 - 1, seconds: 9000, wantPoints: "9223372036854775807"},
		{name: "last whole-second duration", locale: "fr", points: 1<<63 - 1, seconds: uint64((1<<63 - 1) / time.Second), wantPoints: "9223372036854775807"},
		{name: "watch duration overflows", locale: "en", points: 1<<63 - 1, seconds: ^uint64(0), wantPoints: "9223372036854775807"},
		{name: "zero balance and duration", locale: "fr", wantPoints: "0"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &variableLoyaltyBalance{balance: loyaltyrpc.Balance{Points: tc.points, PointsExact: tc.wantPoints, WatchSeconds: tc.seconds}}
			deps := engine.Deps{Loyalty: store, Log: zap.NewNop()}
			c := loyaltyChat(viewer, `{"pointsName":"bagels"}`)
			c.Locale = tc.locale

			values, err := variableGroupReader(t, deps, "loyalty", "balance")(context.Background(), c)
			require.NoError(t, err)
			assert.Equal(t, tc.wantPoints, values["points"], "points must remain exact decimal BIGINT text")
			assert.Equal(t, values["duration"], values["watchtime"])
			assert.Equal(t, "bagels", values["pointsname"])

			m := Loyalty(deps)
			palette := module.StringPalette(values)
			assert.Equal(t, texts(runChat(t, m, c, "!points")), []string{palette.ExpandNamespaced("loyalty", i18n.T(c.Locale, "loyalty.points"))})
			assert.Equal(t, texts(runChat(t, m, c, "!watchtime")), []string{palette.ExpandNamespaced("loyalty", i18n.T(c.Locale, "loyalty.watchtime"))})
			assert.Equal(t, 3, store.reads, "one balance read per custom/native response")
		})
	}
}
