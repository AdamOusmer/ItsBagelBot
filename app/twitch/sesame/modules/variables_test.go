// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"context"
	"errors"
	"testing"
	"time"

	"ItsBagelBot/app/twitch/sesame/engine"
	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/internal/domain/modulevars"
	"ItsBagelBot/internal/domain/outgress"
	gossiprpc "ItsBagelBot/internal/domain/rpc/gossip"
	"ItsBagelBot/internal/projection"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/codec"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type variablePublisher struct{ messages []outgress.Message }

func (p *variablePublisher) PublishOwned(_ context.Context, _ string, payload []byte) error {
	var message outgress.Message
	if err := codec.Unmarshal(payload, &message); err != nil {
		return err
	}
	p.messages = append(p.messages, message)
	return nil
}

func (p *variablePublisher) PublishOwnedWithID(ctx context.Context, subject, _ string, payload []byte) error {
	return p.PublishOwned(ctx, subject, payload)
}

func (*variablePublisher) Flush(context.Context) error { return nil }
func (*variablePublisher) Close() error                { return nil }

// Run a saved !rank through ingress decoding, custom command dispatch, module
// gating and the normal chat publisher, using the same module registry as main.
func runVariableCommand(t *testing.T, response string, views []projection.ModuleView, gossip *fakeGossip) string {
	t.Helper()
	pub := &variablePublisher{}
	d := engine.Deps{
		Proj: &fakeProj{commands: map[string]projection.Command{
			"rank": {Name: "rank", Response: response, IsActive: true, Perm: "everyone"},
		}, modules: views},
		Gossip: gossip, Pub: pub, Live: &fakeLive{live: true}, Greet: &fakeGreet{},
		Special: engine.NewSpecialSet(""), Cooldown: engine.NoopCooldown{}, Log: zap.NewNop(),
	}
	pipeline := engine.NewPipeline(d, engine.NewRegistry(d.Log, All(d)...), engine.Config{OutgressStandard: "outgress.standard"})
	t.Cleanup(pipeline.Close)
	body := []byte(`{"type":"channel.chat.message","lane":"standard","broadcaster_user_id":"2","broadcaster_user_login":"streamer","chatter_user_id":"9","chatter_user_login":"viewer","text":"!rank"}`)
	require.NoError(t, pipeline.Process(bus.NewMessage("module-variable-test", body)))
	require.Len(t, pub.messages, 1)
	var payload struct {
		Message string `json:"message"`
	}
	require.NoError(t, codec.Unmarshal(pub.messages[0].Payload, &payload))
	return payload.Message
}

func enabledVariables(name, config string) projection.ModuleView {
	return projection.ModuleView{Name: name, IsEnabled: true, Configs: []byte(config)}
}

func TestCustomRankCommandReadsValorantFactsOnce(t *testing.T) {
	gossip := &fakeGossip{replies: map[string]any{"valorant.rank": valRankReply()}}
	response := "{valorant:player}: {valorant:tier} ({valorant:rr} RR), {valorant:rank:rr}, {valorant:lastchange}"
	view := enabledVariables("valorant", `{"account":"Frosty#EUW1","region":"eu","platform":"console","rankEnabled":"off"}`)
	assert.Equal(t, "Frosty#EUW1: Immortal 2 (67 RR), 67, -12", runVariableCommand(t, response, []projection.ModuleView{view}, gossip))
	require.Len(t, gossip.calls, 1)
	assert.Equal(t, "valorant", gossip.calls[0].provider)
	assert.Equal(t, "rank", gossip.calls[0].endpoint)
	assert.Equal(t, "Frosty#EUW1", gossip.calls[0].req.Account)
	assert.Equal(t, "eu", gossip.calls[0].req.Region)
	assert.Equal(t, "console", gossip.calls[0].req.Platform)
}

func TestCustomModuleFactsRequireEnabledOptInModule(t *testing.T) {
	for _, tc := range []struct {
		name  string
		views []projection.ModuleView
	}{
		{name: "never enabled"},
		{name: "disabled", views: []projection.ModuleView{{Name: "valorant", IsEnabled: false}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gossip := &fakeGossip{replies: map[string]any{"valorant.rank": valRankReply()}}
			response := "{valorant:tier|unknown} {valorant:rank:rr}"
			assert.Equal(t, response, runVariableCommand(t, response, tc.views, gossip))
			assert.Empty(t, gossip.calls, "disabled modules must never fetch upstream facts")
		})
	}
}

func TestCustomModuleFactsSelectExplicitViewsAndMixModules(t *testing.T) {
	gossip := &fakeGossip{replies: map[string]any{
		"valorant.rank":    valRankReply(),
		"valorant.account": gossiprpc.ValorantAccountReply{Player: "Frosty#EUW1", AccountLevel: 201},
		"codm.profile":     gossiprpc.CODMProfileReply{Player: "Toast", Level: 77},
	}}
	views := []projection.ModuleView{enabledVariables("valorant", `{"account":"Frosty#EUW1"}`), enabledVariables("codm", `{"account":"Toast"}`)}
	response := "{valorant:tier}, level {valorant:account:level}; {codm:player} level {codm:level}"
	assert.Equal(t, "Immortal 2, level 201; Toast level 77", runVariableCommand(t, response, views, gossip))
	assert.Len(t, gossip.calls, 3)
}

func TestCustomModuleFactsUseFallbackForUnavailableData(t *testing.T) {
	for _, tc := range []struct {
		name  string
		reply gossiprpc.ValorantRankReply
		err   error
		want  string
	}{
		{name: "unranked", reply: gossiprpc.ValorantRankReply{Player: "Toast#NA1", Unranked: true}, want: "Unranked: no RR"},
		{name: "provider error", reply: gossiprpc.ValorantRankReply{Error: "account missing"}, want: "unavailable: no RR"},
		{name: "transport error", err: errors.New("gossip down"), want: "unavailable: no RR"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gossip := &fakeGossip{replies: map[string]any{"valorant.rank": tc.reply}, err: tc.err}
			response := "{valorant:tier|unavailable}: {valorant:rr|no RR}"
			assert.Equal(t, tc.want, runVariableCommand(t, response, []projection.ModuleView{enabledVariables("valorant", "")}, gossip))
			assert.Len(t, gossip.calls, 1)
		})
	}
}

func TestCustomNamespacedTimeWorksBesideLegacyTime(t *testing.T) {
	gossip := &fakeGossip{}
	response := "{time} on {time:date} in {time:timezone}"
	views := []projection.ModuleView{enabledVariables("time", `{"timezone":"UTC","format":"24"}`)}
	assert.Regexp(t, `^[0-2][0-9]:[0-5][0-9] on [A-Z][a-z]+, [A-Z][a-z]+ [0-9]{1,2} in UTC$`, runVariableCommand(t, response, views, gossip))
	assert.Empty(t, gossip.calls)
}

func TestCustomModuleFactsLeaveUnknownFieldsLiteral(t *testing.T) {
	gossip := &fakeGossip{replies: map[string]any{"valorant.rank": valRankReply()}}
	response := "{valorant:notafield} {valorant:tier} {valorant.account}"
	assert.Equal(t, "{valorant:notafield} Immortal 2 {valorant.account}", runVariableCommand(t, response, []projection.ModuleView{enabledVariables("valorant", "")}, gossip))
	assert.Len(t, gossip.calls, 1)
}

func TestCustomModuleFactReferencedOnlyByConditionIsFetched(t *testing.T) {
	gossip := &fakeGossip{replies: map[string]any{"valorant.rank": valRankReply()}}
	response := "{if:valorant:rank:tier=Immortal 2:high rank:keep climbing}"
	assert.Equal(t, "high rank", runVariableCommand(t, response, []projection.ModuleView{enabledVariables("valorant", "")}, gossip))
	assert.Len(t, gossip.calls, 1)
}

// The shared catalogue is the public contract. Guard module registration and
// typed read palettes together, so a dashboard token cannot silently drift away
// from the registered runtime field or its reader.
func TestModuleVariableCatalogueMatchesRegisteredGroups(t *testing.T) {
	d := engine.Deps{Log: zap.NewNop(), Special: engine.NewSpecialSet(""), Live: &fakeLive{}, Greet: &fakeGreet{}}
	registered := make(map[string][]module.VariableGroup)
	for _, mod := range All(d) {
		registered[mod.Name] = mod.Variables
	}
	for _, spec := range modulevars.Catalog() {
		if len(spec.Groups) == 0 {
			continue
		}
		t.Run(spec.ID, func(t *testing.T) {
			groups, found := registered[spec.ID]
			require.True(t, found, "catalogue namespace must be registered by All")
			require.Len(t, groups, len(spec.Groups))
			for i, expected := range spec.Groups {
				assert.Equal(t, expected.Name, groups[i].Name)
				assert.Equal(t, expected.Fields, groups[i].Fields)
			}
		})
	}
}

func TestModuleGameReadersExposeEveryCatalogueField(t *testing.T) {
	// Valid empty-valued provider replies still have a complete palette. The
	// snapshot flag keeps session fixtures out of their separate empty state.
	replies := make(map[string]any)
	for provider, endpoints := range map[string][]string{
		"valorant":    {"rank", "matches", "account", "leaderboard", "shop"},
		"codm":        {"profile"},
		"fortnite":    {"stats", "session", "shop"},
		"clashroyale": {"stats", "decks", "ranked", "trophy_road"},
		"urchin":      {"daily", "weekly", "monthly", "sniper", "tags"},
		"hypixel":     {"stats"},
		"mcsr":        {"user", "session", "last_match", "versus", "leaderboard", "weekly_race"},
		"paceman":     {"session", "nethers", "lastfort", "personal_best"},
	} {
		for _, endpoint := range endpoints {
			replies[provider+"."+endpoint] = map[string]any{"has_snapshot": true}
		}
	}
	gossip := &fakeGossip{replies: replies}
	d := engine.Deps{Gossip: gossip, Log: zap.NewNop()}
	for _, spec := range modulevars.Catalog() {
		switch spec.ID {
		case "valorant", "codm", "fortnite", "clashroyale", "urchin", "mcsr":
		default:
			continue
		}
		readers := moduleVariableReaders(d, spec.ID)
		for _, group := range spec.Groups {
			t.Run(spec.ID+":"+group.Name, func(t *testing.T) {
				reader := readers[group.Name]
				require.NotNil(t, reader, "a public game view must have a typed reader")
				c := urchinCtx(`{"account":"Linked","accountUuid":"uuid"}`)
				c.Env.Text = "!rank Alice Bob"
				before := len(gossip.calls)
				values, err := reader(context.Background(), c)
				require.NoError(t, err)
				require.Len(t, gossip.calls, before+1, "a variable group performs one read")
				fields := make([]string, 0, len(values))
				for field := range values {
					fields = append(fields, field)
				}
				assert.ElementsMatch(t, group.Fields, fields, "typed reply palette drifted from the public catalogue")
			})
		}
	}
}

func TestModuleCommandsRenderNamespacedSavedTemplates(t *testing.T) {
	for _, tc := range []struct {
		name, endpoint, config, want string
		reply                        any
		command                      func(*testing.T, engine.GossipCaller) module.Command
	}{
		{
			name: "valorant", endpoint: "valorant.rank", reply: valRankReply(),
			config:  `{"rankMessage":"{valorant:player}: {valorant:tier} ({valorant:rr} RR) {valorant:unknown}"}`,
			want:    "Frosty#EUW1: Immortal 2 (67 RR) {valorant:unknown}",
			command: func(t *testing.T, gw engine.GossipCaller) module.Command { return valCmd(t, gw, "valrank") },
		},
		{
			name: "mcsr", endpoint: "mcsr.user", reply: gossiprpc.McsrUserReply{Nickname: "Feinberg", Elo: 1650, Rank: 12, Wins: 40, Loses: 20, Played: 63},
			config:  `{"eloMessage":"{mcsr:player}: {mcsr:elo} elo, #{mcsr:rank}, {mcsr:draws} draws"}`,
			want:    "Feinberg: 1650 elo, #12, 3 draws",
			command: func(t *testing.T, gw engine.GossipCaller) module.Command { return findCmd(t, mcsrModule(gw), "elo") },
		},
		{
			name: "clashroyale", endpoint: "clashroyale.stats", reply: clashStatsReply(),
			config:  `{"statsMessage":"{clashroyale:player}: {clashroyale:wins}W/{clashroyale:losses}L in {clashroyale:clan}"}`,
			want:    "Bagel: 600W/300L in Bakery",
			command: func(t *testing.T, gw engine.GossipCaller) module.Command { return clashCmd(t, gw, "crstats") },
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gw := &fakeGossip{replies: map[string]any{tc.endpoint: tc.reply}}
			var col collector
			require.NoError(t, tc.command(t, gw).Run(context.Background(), urchinCtx(tc.config), "", col.emit))
			require.Len(t, col.out, 1)
			assert.Equal(t, tc.want, col.out[0].Text)
		})
	}
}

type variableStreamInfo struct {
	result engine.StreamInfoResult
	calls  int
}

func (s *variableStreamInfo) Lookup(context.Context, string, string) (engine.StreamInfoResult, error) {
	s.calls++
	return s.result, nil
}

func TestBuiltinVariableReadersReadFactsWithoutPerformingActions(t *testing.T) {
	started := time.Now().Add(-2*time.Hour - 15*time.Minute)
	followed := time.Now().Add(-24*time.Hour - 3*time.Minute)
	created := time.Now().Add(-48*time.Hour - 5*time.Minute)
	followage := &fakeFollowage{result: engine.FollowageResult{UserFound: true, Following: true, FollowedAt: followed}}
	age := &fakeAccountAge{result: engine.AccountAgeResult{UserFound: true, CreatedAt: created}}
	uptime := &fakeUptime{result: engine.UptimeResult{Live: true, StartedAt: started}}
	stream := &variableStreamInfo{result: engine.StreamInfoResult{UserFound: true, Live: true, Title: "Bagel stream", GameName: "VALORANT", StartedAt: started}}
	d := engine.Deps{Followage: followage, AccountAge: age, Uptime: uptime, StreamInfo: stream}
	c := urchinCtx("")
	for _, tc := range []struct{ namespace, group, field, want string }{
		{"followage", "status", "followedat", followed.UTC().Format(time.RFC3339)},
		{"accountage", "status", "createdat", created.UTC().Format(time.RFC3339)},
		{"uptime", "reply", "uptime", "2 hours, 15 minutes"},
		{"title", "reply", "title", "Bagel stream"},
		{"game", "reply", "game", "VALORANT"},
		{"clip", "reply", "clip", ""},
		{"clip", "reply", "user", "viewer"},
	} {
		t.Run(tc.namespace+":"+tc.field, func(t *testing.T) {
			read := localVariableReaders(d, tc.namespace)[tc.group]
			require.NotNil(t, read)
			values, err := read(context.Background(), c)
			require.NoError(t, err)
			assert.Equal(t, tc.want, values[tc.field])
		})
	}
	assert.Equal(t, "2", followage.got.broadcasterID)
	assert.Equal(t, "9", followage.got.targetID)
	assert.Equal(t, "9", age.got.targetID)
	assert.Equal(t, "2", uptime.got)
	assert.Equal(t, 2, stream.calls)
}

func TestBuiltinVariableReadersLeaveUnavailableDurationsEmpty(t *testing.T) {
	d := engine.Deps{
		Followage:  &fakeFollowage{result: engine.FollowageResult{UserFound: true, Following: false}},
		AccountAge: &fakeAccountAge{result: engine.AccountAgeResult{UserFound: false}},
		Uptime:     &fakeUptime{result: engine.UptimeResult{Live: false}},
	}
	for _, namespace := range []string{"followage", "accountage", "uptime"} {
		group := "status"
		if namespace == "uptime" {
			group = "reply"
		}
		values, err := localVariableReaders(d, namespace)[group](context.Background(), urchinCtx(""))
		require.NoError(t, err)
		assert.Empty(t, values[namespace])
	}
}

func TestTriggersRenderNamespacedChannelAndUser(t *testing.T) {
	c := triggersCtx("hello", "hello => Hi {triggers:user}, welcome to {triggers:channel}!")
	c.Env.BroadcasterUserLogin = "bagel_stream"
	var col collector
	require.NoError(t, triggersHandler(t)(context.Background(), c, col.emit))
	require.Len(t, col.out, 1)
	assert.Equal(t, "Hi Bob, welcome to bagel_stream!", col.out[0].Text)
}

func TestWagerVariableNamespacesRequireEnabledLoyaltyParent(t *testing.T) {
	for _, namespace := range []string{"gamble", "duel"} {
		for _, tc := range []struct {
			name         string
			parent       *projection.ModuleView
			wantFallback bool
		}{
			{name: "parent never enabled"},
			{name: "parent disabled", parent: &projection.ModuleView{Name: "loyalty", IsEnabled: false}},
			{name: "parent enabled", parent: &projection.ModuleView{Name: "loyalty", IsEnabled: true}, wantFallback: true},
		} {
			t.Run(namespace+"/"+tc.name, func(t *testing.T) {
				views := []projection.ModuleView{enabledVariables(namespace, "")}
				if tc.parent != nil {
					views = append(views, *tc.parent)
				}
				response := "{" + namespace + ":user|No wager in progress}"
				want := response
				if tc.wantFallback {
					want = "No wager in progress"
				}
				gossip := &fakeGossip{}
				assert.Equal(t, want, runVariableCommand(t, response, views, gossip))
				assert.Empty(t, gossip.calls)
			})
		}
	}
}

func TestDisabledTimeNamespaceDoesNotFallThroughToPlaceLookup(t *testing.T) {
	gossip := &fakeGossip{}
	views := []projection.ModuleView{{Name: "time", IsEnabled: false, Configs: []byte(`{"format":"24"}`)}}
	response := "{time:date|disabled}/{time:timezone}/{if:time:date:yes:no}/{time:Tokyo}"
	result := runVariableCommand(t, response, views, gossip)
	assert.Regexp(t, `^\{time:date\|disabled\}/\{time:timezone\}/\{if:time:date:yes:no\}/[0-2][0-9]:[0-5][0-9]$`, result)
	assert.Empty(t, gossip.calls)
}
