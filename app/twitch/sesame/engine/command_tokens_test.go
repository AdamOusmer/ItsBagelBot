// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"cmp"
	"errors"
	"strconv"
	"testing"
	"time"

	"ItsBagelBot/app/twitch/sesame/engine/scope"
	"ItsBagelBot/internal/domain/event/lane"
	gossiprpc "ItsBagelBot/internal/domain/rpc/gossip"
	loyaltyrpc "ItsBagelBot/internal/domain/rpc/loyalty"
	modulesrpc "ItsBagelBot/internal/domain/rpc/modules"
	"ItsBagelBot/internal/projection"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

const tokenBotID = "555"

type tokenFixture struct {
	response   string
	line       string
	uses       int64
	modules    map[string]projection.ModuleView
	proj       projection.Reader
	speakers   []chatterIdentity
	chatted    []chatterIdentity
	asStreamer bool
	followage  FollowageLookup
	accountAge AccountAgeLookup
	loyalty    LoyaltyStore
	stream     StreamInfoLookup
	counts     ChannelCountsLookup
	quotes     QuotesStore
	gossip     GossipCaller
	emotes     scope.EmoteSource
	viewers    ViewerLookup
}

func (f tokenFixture) pipeline(t *testing.T) *Pipeline {
	t.Helper()
	proj := f.proj
	if proj == nil {
		proj = fakeReader{
			cmd:      projection.Command{Name: "brag", Response: f.response, IsActive: true, Perm: "everyone", Uses: f.uses},
			cmdFound: true,
			modules:  f.modules,
		}
	}
	d := Deps{
		Proj: proj, Live: liveAlways{}, Cooldown: NoopCooldown{}, Pub: &fakePublisher{}, Log: zap.NewNop(),
		Followage: f.followage, AccountAge: f.accountAge, Loyalty: f.loyalty, StreamInfo: f.stream,
		ChannelCounts: f.counts, Quotes: f.quotes, Gossip: f.gossip, Emotes: f.emotes, Viewers: f.viewers,
	}
	p := NewPipeline(d, NewRegistry(zap.NewNop()), Config{
		OutgressPremium: premiumSubj, OutgressStandard: standardSubj, BotID: tokenBotID,
	})
	for _, who := range f.speakers {
		p.roster.Observe(123, who)
	}
	for _, who := range f.chatted {
		require.NoError(t, p.Process(envelopeMsg(t, "uuid-"+who.id, map[string]any{
			"chatter_user_id": who.id, "chatter_user_login": who.login, "chatter_user_name": who.name, "text": "hi",
		})))
	}
	return p
}

func (f tokenFixture) envelope() lane.Envelope {
	env := chatEnv(cmp.Or(f.line, "!brag"), "")
	if f.asStreamer {
		env.ChatterUserID, env.ChatterUserLogin = "123", "streamer"
	}
	return env
}

func expandViewer(t *testing.T, p *Pipeline, env lane.Envelope) string {
	t.Helper()
	got := replies(t, p, env)
	require.Len(t, got, 1)
	return got[0].Text
}

type tokenCase struct {
	name      string
	fix       tokenFixture
	want      string
	wantRe    string
	runs      int
	reads     func() []any
	wantReads []any
}

func (tc tokenCase) assertText(t *testing.T, got string) {
	t.Helper()
	if tc.wantRe != "" {
		assert.Regexp(t, tc.wantRe, got)
		return
	}
	assert.Equal(t, tc.want, got)
}

func TestCommandTokensExpand(t *testing.T) {
	var cases []tokenCase
	for _, family := range [][]tokenCase{
		viewerSpanCases(), viewerLookupCases(), loyaltyTokenCases(), channelCountCases(), channelStreamCases(),
		channelReadCostCases(), chatterTokenCases(), emoteTokenCases(), randomViewerTokenCases(), usesTokenCases(),
		quoteTokenCases(), songTokenCases(), timeTokenCases(), counterTokenCases(),
	} {
		cases = append(cases, family...)
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := tc.fix.pipeline(t)
			for range max(tc.runs, 1) {
				tc.assertText(t, expandViewer(t, p, tc.fix.envelope()))
			}
			if tc.reads != nil {
				assert.Equal(t, tc.wantReads, tc.reads())
			}
		})
	}
}

func viewerSpanCases() []tokenCase {
	followedAt := time.Now().Add(-90 * 24 * time.Hour)
	createdAt := time.Now().Add(-3 * 365 * 24 * time.Hour)
	follows := func(result FollowageResult) *stubFollowage { return &stubFollowage{result: result} }
	followed := FollowageResult{UserFound: true, Following: true, FollowedAt: followedAt}
	hourAgo := FollowageResult{UserFound: true, Following: true, FollowedAt: time.Now().Add(-time.Hour)}

	return []tokenCase{
		{
			name: "followage and account age render the humanized span",
			fix: tokenFixture{
				response:   "{user} has followed for {followage} on an account {accountage} old",
				followage:  follows(followed),
				accountAge: &stubAccountAge{result: AccountAgeResult{UserFound: true, CreatedAt: createdAt}},
			},
			want: "alice has followed for 3 months on an account 3 years old",
		},
		{
			name: "an explicit module-off leaves the span literal",
			fix: tokenFixture{
				response:  "followed for {followage|a while}",
				modules:   map[string]projection.ModuleView{FollowageModuleName: off()},
				followage: follows(followed),
			},
			want: "followed for {followage|a while}",
		},
		{
			name: "a viewer who does not follow renders the fallback",
			fix: tokenFixture{
				response:  "followed for {followage|not yet}",
				followage: follows(FollowageResult{UserFound: true}),
			},
			want: "followed for not yet",
		},
		{
			name: "a failed lookup degrades to the fallback, never to a literal",
			fix: tokenFixture{
				response:  "followed for {followage|a while}",
				followage: &stubFollowage{err: errors.New("outgress down")},
			},
			want: "followed for a while",
		},
		{
			name: "a wired reader with no token in the template changes nothing",
			fix:  tokenFixture{response: "just {user}", followage: follows(FollowageResult{})},
			want: "just alice",
		},
		{
			name: "unknown spans stay literal beside a resolved one",
			fix:  tokenFixture{response: "{followage} {followage_years} {pointsrank}", followage: follows(hourAgo)},
			want: "1 hour {followage_years} {pointsrank}",
		},
	}
}

func viewerLookupCases() []tokenCase {
	follows := func(result FollowageResult) *stubFollowage { return &stubFollowage{result: result} }
	hourAgo := FollowageResult{UserFound: true, Following: true, FollowedAt: time.Now().Add(-time.Hour)}
	hourFollow := follows(hourAgo)
	loyalty := &stubLoyalty{balances: map[uint64]loyaltyrpc.Balance{999: {Points: 3, WatchSeconds: 60}}}
	named := &stubFollowage{result: FollowageResult{UserFound: true, Following: true, FollowedAt: time.Now().Add(-48 * time.Hour)}}
	namedLoyalty := &stubLoyalty{balances: map[uint64]loyaltyrpc.Balance{7: {Points: 55}}}
	once := &stubLoyalty{balances: map[uint64]loyaltyrpc.Balance{999: {Points: 10}}}

	return []tokenCase{
		{
			name: "each viewer is looked up once, riding the chatter's own id",
			fix: tokenFixture{
				response:  "{followage} {followage:alice} {followage:@ALICE} {points} {watchtime}",
				modules:   map[string]projection.ModuleView{LoyaltyModuleName: on()},
				followage: hourFollow,
				loyalty:   loyalty,
			},
			want:      "1 hour 1 hour 1 hour 3 1 minute",
			reads:     func() []any { return []any{hourFollow.calls, loyalty.balanceReads} },
			wantReads: []any{[]string{"999/alice"}, []uint64{999}},
		},
		{
			name: "a named viewer resolves through the roster",
			fix: tokenFixture{
				response:  "{followage:@bob} / {points:BOB}",
				modules:   map[string]projection.ModuleView{LoyaltyModuleName: on()},
				followage: named,
				loyalty:   namedLoyalty,
				speakers:  []chatterIdentity{{login: "bob", id: "7", name: "Bob"}},
			},
			want:      "2 days / 55",
			reads:     func() []any { return []any{named.calls, namedLoyalty.balanceReads} },
			wantReads: []any{[]string{"/bob"}, []uint64{7}},
		},
		{
			name: "balance tokens read once however often they render",
			fix: tokenFixture{
				response: "{points} {points} {points}",
				modules:  map[string]projection.ModuleView{LoyaltyModuleName: on()},
				loyalty:  once,
			},
			want:      "10 10 10",
			reads:     func() []any { return []any{once.balanceReads} },
			wantReads: []any{[]uint64{999}},
		},
	}
}

func loyaltyTokenCases() []tokenCase {
	return []tokenCase{
		{
			name: "loyalty tokens render the balance and the currency name",
			fix: tokenFixture{
				response: "{user}: {points} {pointsname}, {watchtime} watched",
				modules:  map[string]projection.ModuleView{LoyaltyModuleName: loyaltyOn("bagels")},
				loyalty:  &stubLoyalty{balances: map[uint64]loyaltyrpc.Balance{999: {Points: 1280, WatchSeconds: 9000}}},
			},
			want: "alice: 1280 bagels, 2 hours, 30 minutes watched",
		},
		{
			name: "an enabled module with no configured name uses the default",
			fix: tokenFixture{
				response: "{points} {pointsname}",
				modules:  map[string]projection.ModuleView{LoyaltyModuleName: on()},
				loyalty:  &stubLoyalty{balances: map[uint64]loyaltyrpc.Balance{999: {Points: 7}}},
			},
			want: "7 points",
		},
		{
			name: "loyalty off leaves every loyalty token literal",
			fix:  tokenFixture{response: "{points} {pointsname} {watchtime}", loyalty: &stubLoyalty{}},
			want: "{points} {pointsname} {watchtime}",
		},
		{
			name: "a named viewer resolves by lower-cased login through the roster",
			fix: tokenFixture{
				response: "{points:@BOB}",
				modules:  map[string]projection.ModuleView{LoyaltyModuleName: on()},
				loyalty:  &stubLoyalty{balances: map[uint64]loyaltyrpc.Balance{7: {Points: 55}}},
				chatted:  []chatterIdentity{{id: "7", login: "Bob", name: "Bob"}},
			},
			want: "55",
		},
		{
			name: "an unresolvable named viewer renders the fallback",
			fix: tokenFixture{
				response: "ferret_king has {points:ferret_king|no} points",
				modules:  map[string]projection.ModuleView{LoyaltyModuleName: on()},
				loyalty:  &stubLoyalty{},
			},
			want: "ferret_king has no points",
		},
	}
}

func channelCountCases() []tokenCase {
	allModules := &countingReader{fakeReader: fakeReader{
		cmd:      projection.Command{Name: "brag", Response: "{uptime} {title} {game}", IsActive: true, Perm: "everyone"},
		cmdFound: true,
		modules:  map[string]projection.ModuleView{UptimeModuleName: on(), TitleModuleName: on(), GameModuleName: on()},
	}}
	counts := &stubChannelCounts{result: ChannelCountsResult{Followers: 100, FollowersOK: true, Subs: 7, SubsOK: true}}

	return []tokenCase{
		{
			name: "no stream reader wired means no module row is read",
			fix:  tokenFixture{proj: allModules},
			want: "{uptime} {title} {game}",
			reads: func() []any {
				return []any{allModules.moduleReads}
			},
			wantReads: []any{0},
		},
		{
			name:      "count tokens need no module row and share one read",
			fix:       tokenFixture{response: "{followers} followers, {subs} subs", counts: counts},
			want:      "100 followers, 7 subs",
			reads:     func() []any { return []any{counts.calls} },
			wantReads: []any{1},
		},
		{
			name: "a not-OK half of the counts stays literal",
			fix: tokenFixture{
				response: "{followers} {subs}",
				counts:   &stubChannelCounts{result: ChannelCountsResult{Followers: 100, FollowersOK: true}},
			},
			want: "100 {subs}",
		},
		{
			name: "count tokens stay literal without the dependency",
			fix:  tokenFixture{response: "{followers} {subs}"},
			want: "{followers} {subs}",
		},
	}
}

func channelStreamCases() []tokenCase {
	own := map[string]StreamInfoResult{"123/": liveNow()}
	pokimane := StreamInfoResult{UserFound: true, Live: true, GameName: "VALORANT"}

	return []tokenCase{
		{
			name: "the whole family renders from one live session",
			fix: tokenFixture{
				response: "{title} / {game} / {channel.viewers} / {uptime}",
				stream:   &stubStreamInfo{byAddress: own},
			},
			want: "bagel time / Just Chatting / 42 / 2 hours",
		},
		{
			name: "a command's own toggle gates only its own token",
			fix: tokenFixture{
				response: "{title} / {game}",
				modules:  map[string]projection.ModuleView{TitleModuleName: off()},
				stream:   &stubStreamInfo{byAddress: own},
			},
			want: "{title} / Just Chatting",
		},
		{
			name: "the uptime toggle is the one !uptime reads",
			fix: tokenFixture{
				response: "up for {uptime}",
				modules:  map[string]projection.ModuleView{UptimeModuleName: off()},
				stream:   &stubStreamInfo{byAddress: own},
			},
			want: "up for {uptime}",
		},
		{
			name: "an offline channel is zero viewers and an empty uptime",
			fix: tokenFixture{
				response: "{channel.viewers} watching, live {uptime|nope}",
				stream: &stubStreamInfo{byAddress: map[string]StreamInfoResult{
					"123/": {UserFound: true, Title: "back soon", GameName: "Just Chatting"},
				}},
			},
			want: "0 watching, live nope",
		},
		{
			name: "a named channel resolves by login",
			fix: tokenFixture{
				response: "go watch {game:@Pokimane}",
				stream:   &stubStreamInfo{byAddress: map[string]StreamInfoResult{"/pokimane": pokimane}},
			},
			want: "go watch VALORANT",
		},
		{
			name: "a failed read renders the fallback, never the braces",
			fix: tokenFixture{
				response: "playing {game|something}",
				stream:   &stubStreamInfo{err: errors.New("boom")},
			},
			want: "playing something",
		},
	}
}

func channelReadCostCases() []tokenCase {
	pokimane := StreamInfoResult{UserFound: true, Live: true, GameName: "VALORANT"}
	oneRead := &stubStreamInfo{byAddress: map[string]StreamInfoResult{"123/": liveNow(), "/pokimane": pokimane}}
	noToken := &stubStreamInfo{}

	return []tokenCase{
		{
			name: "no reader wired leaves every span literal",
			fix:  tokenFixture{response: "{title} {game} {uptime} {channel.viewers}"},
			want: "{title} {game} {uptime} {channel.viewers}",
		},
		{
			name: "channel tokens cost one read per channel",
			fix: tokenFixture{
				response: "{title} {game} {uptime} {channel.viewers} {game:pokimane}",
				stream:   oneRead,
			},
			want:      "bagel time Just Chatting 2 hours 42 VALORANT",
			reads:     func() []any { return []any{oneRead.calls} },
			wantReads: []any{[]string{"123/", "/pokimane"}},
		},
		{
			name:      "a template naming no channel token never reads",
			fix:       tokenFixture{response: "hi", stream: noToken},
			want:      "hi",
			reads:     func() []any { return []any{noToken.calls} },
			wantReads: []any{[]string(nil)},
		},
	}
}

func numberedChatters(n int) []chatterIdentity {
	chatters := make([]chatterIdentity, n)
	for i := range chatters {
		chatters[i] = chatterIdentity{id: strconv.Itoa(i + 1), login: "viewer" + strconv.Itoa(i), name: "Viewer"}
	}
	return chatters
}

func chatterTokenCases() []tokenCase {
	cases := append(chatterTokenRows(), rosterCases()...)
	for i := range cases {
		cases[i].fix.asStreamer = true
	}
	return cases
}

func chatterTokenRows() []tokenCase {
	bot := chatterIdentity{id: tokenBotID, login: "bagelbot", name: "BagelBot"}
	streamer := chatterIdentity{id: "123", login: "streamer", name: "Streamer"}
	sam := chatterIdentity{id: "42", login: "sam", name: "Sam"}

	return []tokenCase{
		{
			name: "the chatters token counts the roster including whoever invoked it",
			fix: tokenFixture{
				response: "{chatters} of us here",
				speakers: []chatterIdentity{{id: "1", login: "sam", name: "Sam"}, {id: "2", login: "alex", name: "Alex"}},
			},
			want: "3 of us here",
		},
		{
			name: "the chatters token counts only the invoker when nobody else spoke",
			fix:  tokenFixture{response: "{chatters} of us here"},
			want: "1 of us here",
		},
		{
			name: "a random chatter excludes the bot and the broadcaster",
			fix:  tokenFixture{response: "say hi to {random.chatter}", speakers: []chatterIdentity{bot, streamer, sam}},
			want: "say hi to Sam",
		},
		{
			name: "the chatters count is the whole room",
			fix:  tokenFixture{response: "{chatters}", speakers: []chatterIdentity{bot, streamer, sam}},
			want: "3",
		},
		{
			name: "a random chatter falls back to the login",
			fix: tokenFixture{
				response: "hi {random.chatter}",
				speakers: []chatterIdentity{{id: "42", login: "sam"}},
			},
			want: "hi sam",
		},
		{
			name: "a random chatter's drawn name is sanitized",
			fix: tokenFixture{
				response: "hi {random.chatter}",
				speakers: []chatterIdentity{{id: "42", login: "sam", name: "/me waves"}},
			},
			want: "hi me waves",
		},
		{
			name: "a random chatter renders its fallback when the room is empty",
			fix:  tokenFixture{response: "hi {random.chatter|everyone}"},
			want: "hi everyone",
		},
		{
			name: "misspelled chatter tokens stay literal",
			fix: tokenFixture{
				response: "{chatters:5} {random.chatter:mods} {chatter}",
				speakers: []chatterIdentity{sam},
			},
			want: "{chatters:5} {random.chatter:mods} {chatter}",
		},
	}
}

func rosterCases() []tokenCase {
	return []tokenCase{
		{
			name: "chatters who spoke are drawn by their display name",
			fix: tokenFixture{
				response: "{chatters} here, hi {random.chatter}",
				chatted:  []chatterIdentity{{id: "7", login: "bob", name: "Bob"}},
			},
			want: "2 here, hi Bob",
		},
		{
			name: "an identity without a login or a numeric id is not counted",
			fix: tokenFixture{
				response: "{chatters}",
				chatted:  []chatterIdentity{{id: "9", name: "X"}, {id: "10", login: "", name: "Y"}, {id: "zero", login: "x", name: "X"}, {login: "z", name: "Z"}},
			},
			want: "1",
		},
		{
			name: "a newer display name replaces the stored one",
			fix: tokenFixture{
				response: "{random.chatter}",
				chatted:  []chatterIdentity{{id: "7", login: "bob", name: "Bob"}, {id: "7", login: "bob", name: "Robert"}},
			},
			want: "Robert",
		},
		{
			name: "an observation without a display name keeps the stored one",
			fix: tokenFixture{
				response: "{random.chatter}",
				chatted:  []chatterIdentity{{id: "7", login: "bob", name: "Robert"}, {id: "7", login: "bob"}},
			},
			want: "Robert",
		},
		{
			name: "the roster stays bounded per channel",
			fix:  tokenFixture{response: "{chatters}", chatted: numberedChatters(rosterCapacityPerChannel + 10)},
			want: strconv.Itoa(rosterCapacityPerChannel),
		},
	}
}

func emoteTokenCases() []tokenCase {
	pagman := fakeEmotes{sets: scope.EmoteSets{SevenTV: []string{"PagMan"}}}

	return []tokenCase{
		{
			name: "emote list tokens print their provider",
			fix: tokenFixture{
				response: "7tv: {7tvemotes} bttv: {bttvemotes} ffz: {ffzemotes}",
				emotes: fakeEmotes{sets: scope.EmoteSets{
					SevenTV: []string{"PagMan", "Clap"}, BTTV: []string{"KEKW"}, FFZ: []string{"LUL"},
				}},
			},
			want: "7tv: PagMan Clap bttv: KEKW ffz: LUL",
		},
		{
			name: "emote tokens need no module",
			fix:  tokenFixture{response: "{random.emote}", emotes: pagman},
			want: "PagMan",
		},
		{
			name: "emote tokens render their fallback when nothing is loaded",
			fix:  tokenFixture{response: "{7tvemotes|nothing loaded} {random.emote|🥯}", emotes: fakeEmotes{}},
			want: "nothing loaded 🥯",
		},
		{
			name: "emote tokens stay literal without a source",
			fix:  tokenFixture{response: "{7tvemotes} {random.emote}"},
			want: "{7tvemotes} {random.emote}",
		},
		{
			name: "misspelled emote tokens stay literal",
			fix:  tokenFixture{response: "{7tvemotes:100} {random.emote:7tv} {twitchemotes}", emotes: pagman},
			want: "{7tvemotes:100} {random.emote:7tv} {twitchemotes}",
		},
		{
			name: "a random emote draws per span",
			fix:  tokenFixture{response: "{random.emote} {random.emote}", emotes: pagman},
			want: "PagMan PagMan",
		},
	}
}

func randomViewerTokenCases() []tokenCase {
	entry := func(id uint64, login string) chattersSnapshotEntry {
		return chattersSnapshotEntry{ID: id, Login: login, Name: login}
	}
	warm := &stubViewerLookup{state: viewerSnapshotOK, entries: []chattersSnapshotEntry{
		entry(999, "alice"), entry(555, "bagelbot"), entry(123, "streamer"), entry(42, "lurker"),
	}}
	room := []chatterIdentity{{id: "999", login: "alice", name: "Alice"}, {id: "42", login: "sam", name: "Sam"}}

	return []tokenCase{
		{
			name:      "a random viewer excludes the sender, the bot and the broadcaster",
			fix:       tokenFixture{response: "say hi to {random.viewer}", viewers: warm},
			want:      "say hi to lurker",
			reads:     func() []any { return []any{warm.calls} },
			wantReads: []any{1},
		},
		{
			name: "a cold snapshot degrades to the roster",
			fix: tokenFixture{
				response: "say hi to {random.viewer}", speakers: room,
				viewers: &stubViewerLookup{state: viewerSnapshotCold},
			},
			want: "say hi to Sam",
		},
		{
			name: "a cold snapshot and an empty room render the fallback",
			fix: tokenFixture{
				response: "hi {random.viewer|someone}",
				viewers:  &stubViewerLookup{state: viewerSnapshotCold},
			},
			want: "hi someone",
		},
		{
			name: "a missing scope degrades to the roster",
			fix: tokenFixture{
				response: "say hi to {random.viewer}", speakers: room,
				viewers: &stubViewerLookup{state: viewerSnapshotMissingScope},
			},
			want: "say hi to Sam",
		},
		{
			name: "a random viewer renders the fallback without the dependency",
			fix:  tokenFixture{response: "hi {random.viewer|nobody}"},
			want: "hi nobody",
		},
	}
}

func usesTokenCases() []tokenCase {
	return []tokenCase{
		{
			name: "the uses token renders the projected count",
			fix:  tokenFixture{response: "this has been used {uses} times", uses: 128},
			want: "this has been used 128 times",
		},
		{
			name: "the uses token excludes the current run",
			fix:  tokenFixture{response: "{uses}", uses: 4},
			want: "4",
			runs: 2,
		},
		{
			name: "the uses token renders zero for a never-run command",
			fix:  tokenFixture{response: "used {uses|never} times"},
			want: "used 0 times",
		},
		{
			name: "the uses token with a payload stays literal",
			fix:  tokenFixture{response: "{uses:other}", uses: 9},
			want: "{uses:other}",
		},
	}
}

func quoteTokenCases() []tokenCase {
	hi := &stubQuotes{byNum: map[uint64]modulesrpc.Quote{1: quoteAt(1, "hi")}}
	bagels := map[uint64]modulesrpc.Quote{12: quoteAt(12, "bagels win")}
	draws := &stubQuotes{random: []modulesrpc.Quote{quoteAt(1, "first"), quoteAt(2, "second")}}
	quotesOn := map[string]projection.ModuleView{QuotesModuleName: on()}

	return []tokenCase{
		{
			name: "a numbered quote renders the line !quote prints",
			fix: tokenFixture{
				response: "remember: {quote:12}", modules: quotesOn, quotes: &stubQuotes{byNum: bagels},
			},
			want: "remember: Quote #12: bagels win (2026-01-31)",
		},
		{
			name: "a quote nobody saved renders its fallback",
			fix: tokenFixture{
				response: "remember: {quote:99|nothing yet}", modules: quotesOn, quotes: &stubQuotes{},
			},
			want: "remember: nothing yet",
		},
		{
			name: "a failed quote read renders empty, never an excuse",
			fix: tokenFixture{
				response: "remember:{quote:12}", modules: quotesOn, quotes: &stubQuotes{err: errors.New("rpc down")},
			},
			want: "remember:",
		},
		{
			name: "the quotes module off leaves the span literal",
			fix: tokenFixture{
				response: "remember: {quote:12}",
				modules:  map[string]projection.ModuleView{QuotesModuleName: off()},
				quotes:   &stubQuotes{byNum: bagels},
			},
			want: "remember: {quote:12}",
		},
		{
			name: "a channel that never enabled quotes leaves the span literal",
			fix: tokenFixture{
				response: "remember: {quote}", quotes: &stubQuotes{random: []modulesrpc.Quote{quoteAt(3, "hi")}},
			},
			want: "remember: {quote}",
		},
		{
			name: "an unknown token stays literal beside a resolved one",
			fix:  tokenFixture{response: "{quote:1} {quotes} {songs}", modules: quotesOn, quotes: hi},
			want: "Quote #1: hi (2026-01-31) {quotes} {songs}",
		},
		{
			name: "a random quote draws independently per span",
			fix: tokenFixture{
				response: "{quote} then {quote}", modules: quotesOn, quotes: draws,
			},
			want:      "Quote #1: first (2026-01-31) then Quote #2: second (2026-01-31)",
			reads:     func() []any { return []any{draws.draws} },
			wantReads: []any{2},
		},
	}
}

func songTokenCases() []tokenCase {
	fanQuotes := &stubQuotes{byNum: map[uint64]modulesrpc.Quote{7: quoteAt(7, "hi")}}
	fanGossip := &stubGossip{reply: playing("Bagel Song", "The Ovens")}
	idleQuotes, idleGossip := &stubQuotes{}, &stubGossip{}
	songOn := map[string]projection.ModuleView{SongQueueModuleName: on()}

	return []tokenCase{
		{
			name: "the playing track renders whole and in halves",
			fix: tokenFixture{
				response: "{song} - {song.title} / {song.artist}", modules: songOn,
				gossip: &stubGossip{reply: playing("Bagel Song", "The Ovens", "Yeast")},
			},
			want: "Bagel Song by The Ovens, Yeast - Bagel Song / The Ovens, Yeast",
		},
		{
			name: "an idle player renders the fallback, not an upstream sentence",
			fix: tokenFixture{
				response: "now playing: {song|nothing right now}", modules: songOn, gossip: &stubGossip{},
			},
			want: "now playing: nothing right now",
		},
		{
			name: "a channel with no spotify connection renders empty",
			fix: tokenFixture{
				response: "now playing:{song}", modules: songOn,
				gossip: &stubGossip{reply: gossiprpc.SpotifyNowPlayingReply{Error: "no Spotify app set up"}},
			},
			want: "now playing:",
		},
		{
			name: "the song module off leaves every spelling literal",
			fix: tokenFixture{
				response: "{song} {song.title}",
				modules:  map[string]projection.ModuleView{SongQueueModuleName: off()},
				gossip:   &stubGossip{reply: playing("Bagel Song", "The Ovens")},
			},
			want: "{song} {song.title}",
		},
		{
			name: "module tokens fan out once per family",
			fix: tokenFixture{
				response: "{quote:7} {quote:7} {song} {song.title} {song.artist}",
				modules:  map[string]projection.ModuleView{QuotesModuleName: on(), SongQueueModuleName: on()},
				quotes:   fanQuotes, gossip: fanGossip,
			},
			want:      "Quote #7: hi (2026-01-31) Quote #7: hi (2026-01-31) Bagel Song by The Ovens Bagel Song The Ovens",
			reads:     func() []any { return []any{fanQuotes.gets, fanGossip.calls} },
			wantReads: []any{[]uint64{7}, []GossipRoute{spotifyNowPlaying}},
		},
		{
			name:      "unused module tokens cost no upstream call",
			fix:       tokenFixture{response: "plain text", quotes: idleQuotes, gossip: idleGossip},
			want:      "plain text",
			reads:     func() []any { return []any{idleQuotes.draws, idleGossip.calls} },
			wantReads: []any{0, []GossipRoute(nil)},
		},
	}
}

func timeTokenCases() []tokenCase {
	return []tokenCase{
		{
			name: "an enabled time module with no timezone renders its fallback",
			fix: tokenFixture{
				response: "it is {time|anyone's guess}", modules: map[string]projection.ModuleView{TimeModuleName: on()},
			},
			want: "it is anyone's guess",
		},
		{
			name: "the time module off leaves the span literal",
			fix: tokenFixture{
				response: "it is {time}", modules: map[string]projection.ModuleView{TimeModuleName: off()},
			},
			want: "it is {time}",
		},
		{
			name: "the time token renders the configured 12 hour clock face",
			fix: tokenFixture{
				response: "it is {time} here", modules: map[string]projection.ModuleView{TimeModuleName: timeOn("UTC", "")},
			},
			wantRe: `^it is ([1-9]|1[0-2]):[0-5][0-9] (AM|PM) here$`,
		},
		{
			name: "the time token renders the configured 24 hour clock face",
			fix: tokenFixture{
				response: "it is {time} here", modules: map[string]projection.ModuleView{TimeModuleName: timeOn("UTC", "24")},
			},
			wantRe: `^it is [0-2][0-9]:[0-5][0-9] here$`,
		},
		{
			name: "an unknown configured zone renders the fallback",
			fix: tokenFixture{
				response: "it is {time|anyone's guess}",
				modules:  map[string]projection.ModuleView{TimeModuleName: timeOn("Mars/Olympus", "24")},
			},
			want: "it is anyone's guess",
		},
		{
			name:   "a time place answers ungated",
			fix:    tokenFixture{response: "it is {time:Tokyo} in Tokyo"},
			wantRe: `^it is ([1-9]|1[0-2]):[0-5][0-9] (AM|PM) in Tokyo$`,
		},
		{
			name: "a time place uses the configured clock face",
			fix: tokenFixture{
				response: "it is {time:Tokyo}", modules: map[string]projection.ModuleView{TimeModuleName: timeOn("UTC", "24")},
			},
			wantRe: `^it is [0-2][0-9]:[0-5][0-9]$`,
		},
		{
			name: "an unknown time place renders its fallback",
			fix:  tokenFixture{response: "{time:nowhere|unknown place}"},
			want: "unknown place",
		},
		{
			name:   "distinct time places are capped at three",
			fix:    tokenFixture{response: "{time:tokyo}|{time:paris}|{time:cairo}|{time:lima}"},
			wantRe: `^[^|]+\|[^|]+\|[^|]+\|$`,
		},
	}
}

func counterTokenCases() []tokenCase {
	deaths := &stubLoyalty{counters: map[string]int64{"deaths": 42}}
	repeated := &stubLoyalty{counters: map[string]int64{"deaths": 42}}
	spelled := &stubLoyalty{counters: map[string]int64{"deaths": 42}}
	mentioned := &stubLoyalty{counters: map[string]int64{"shutups": 42}}
	stranger := &stubLoyalty{counters: map[string]int64{"shutups": 42}}
	degenerate := &stubLoyalty{}

	return []tokenCase{
		{
			name: "a read-only counter renders without bumping",
			fix:  tokenFixture{response: "deaths so far: {count:deaths}", loyalty: deaths},
			want: "deaths so far: 42",
			reads: func() []any {
				return []any{deaths.bumps}
			},
			wantReads: []any{[]CounterBump(nil)},
		},
		{
			name: "a counter nobody has bumped renders its fallback",
			fix:  tokenFixture{response: "deaths so far: {count:deaths|none yet}", loyalty: &stubLoyalty{}},
			want: "deaths so far: none yet",
		},
		{
			name: "no loyalty store wired leaves the read-only span literal",
			fix:  tokenFixture{response: "deaths so far: {count:deaths}"},
			want: "deaths so far: {count:deaths}",
		},
		{
			name:      "counter and count are read aliases of one lookup",
			fix:       tokenFixture{response: "{counter:deaths} deaths ({count:Deaths} recorded)", loyalty: spelled},
			want:      "42 deaths (42 recorded)",
			reads:     func() []any { return []any{spelled.peeks, spelled.bumps} },
			wantReads: []any{[]string{"deaths"}, []CounterBump(nil)},
		},
		{
			name:      "two spellings of one counter cost one read and never bump",
			fix:       tokenFixture{response: "{count:deaths} {count:deaths}", loyalty: repeated},
			want:      "42 42",
			reads:     func() []any { return []any{repeated.peeks, repeated.bumps} },
			wantReads: []any{[]string{"deaths"}, []CounterBump(nil)},
		},
		{
			name: "a targeted counter read keys on the mentioned viewer",
			fix: tokenFixture{
				response: "@{target} has been told {counter:target:shutups} times",
				line:     "!brag @bob",
				loyalty:  mentioned,
				speakers: []chatterIdentity{{login: "bob", id: "7", name: "Bob"}},
			},
			want:      "@bob has been told 42 times",
			reads:     func() []any { return []any{mentioned.bumps, mentioned.peeks} },
			wantReads: []any{[]CounterBump(nil), []string{"shutups"}},
		},
		{
			name: "an unresolved target falls back to the sender",
			fix: tokenFixture{
				response: "{target}: {counter:target:shutups}", line: "!brag @stranger", loyalty: stranger,
			},
			want:      "stranger: 42",
			reads:     func() []any { return []any{stranger.bumps} },
			wantReads: []any{[]CounterBump(nil)},
		},
		{
			name: "an empty targeted counter name stays visible and reads nothing",
			fix: tokenFixture{
				response: "x{counter:target:}", line: "!brag @bob", loyalty: degenerate,
			},
			want:      "x{counter:target:}",
			reads:     func() []any { return []any{degenerate.bumps, degenerate.peeks} },
			wantReads: []any{[]CounterBump(nil), []string(nil)},
		},
	}
}
