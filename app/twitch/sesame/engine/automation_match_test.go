// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"sync"
	"testing"
	"time"

	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/internal/domain/automation"
	"ItsBagelBot/internal/domain/event/lane"
	automationrpc "ItsBagelBot/internal/domain/rpc/automation"
	automationsrpc "ItsBagelBot/internal/domain/rpc/automations"
	loyaltyrpc "ItsBagelBot/internal/domain/rpc/loyalty"
	"ItsBagelBot/internal/projection"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/codec"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type fakeAutoSource struct {
	list []automation.Automation
	err  error
}

func (s *fakeAutoSource) Automations(context.Context, uint64) ([]automation.Automation, error) {
	return s.list, s.err
}

func (s *fakeAutoSource) AutomationIndex(context.Context, uint64) (*automation.TriggerIndex, error) {
	return automation.CompileIndex(s.list), s.err
}

type recordingRunLog struct {
	mu   sync.Mutex
	recs []automationrpc.RunRecord
}

func (r *recordingRunLog) Record(_ context.Context, _ uint64, rec automationrpc.RunRecord) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.recs = append(r.recs, rec)
}

func (r *recordingRunLog) Recent(context.Context, uint64, string, int) ([]automationrpc.RunRecord, error) {
	return nil, nil
}

func (r *recordingRunLog) summary() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []string
	for _, rec := range r.recs {
		line := rec.Name + ":" + rec.Outcome
		if rec.Reason != "" {
			line += ":" + rec.Reason
		}
		out = append(out, line)
	}
	return out
}

type recordingCooldown struct {
	NoopCooldown
	deny bool
	keys []string
}

func (c *recordingCooldown) Allow(_ context.Context, key string, _ time.Duration) (bool, error) {
	c.keys = append(c.keys, key)
	return !c.deny, nil
}

func (c *recordingCooldown) AllowAll(_ context.Context, windows []CooldownWindow) (bool, error) {
	for _, w := range windows {
		c.keys = append(c.keys, w.Key)
	}
	return !c.deny, nil
}

type autoRig struct {
	automations []automation.Automation
	modules     map[string]projection.ModuleView
	cmd         projection.Command
	cmdFound    bool
	builtin     bool
	nilSource   bool
	cooldown    CooldownStore
	loyalty     LoyaltyStore
	stream      StreamInfoLookup
	intN        int
	pub         *fakePublisher
}

func autoModule(hook *AutomationHook) module.Module {
	b := module.NewModule(AutomationsModuleName, module.KindOptIn)
	hook.Register(b)
	return b.Build()
}

func builtinHi() module.Module {
	b := module.NewModule("", module.KindCore)
	b.Command("hi").Run(func(context.Context, *module.Context, string, module.Emit) error { return nil })
	return b.Build()
}

func (r autoRig) pipeline() (*Pipeline, *recordingRunLog) {
	modules := r.modules
	if modules == nil {
		modules = map[string]projection.ModuleView{AutomationsModuleName: on()}
	}
	hook := &AutomationHook{}
	mods := []module.Module{autoModule(hook)}
	if r.builtin {
		mods = append(mods, builtinHi())
	}
	runs := &recordingRunLog{}
	pub := r.pub
	if pub == nil {
		pub = &fakePublisher{}
	}
	cooldown := r.cooldown
	if cooldown == nil {
		cooldown = NoopCooldown{}
	}
	d := Deps{
		Proj:       fakeReader{modules: modules, cmd: r.cmd, cmdFound: r.cmdFound},
		Live:       liveAlways{},
		Cooldown:   cooldown,
		Pub:        pub,
		Loyalty:    r.loyalty,
		StreamInfo: r.stream,
		Log:        zap.NewNop(),
		Automations: AutomationDeps{
			Runs: runs,
		},
	}
	if !r.nilSource {
		d.Automations.Source = &fakeAutoSource{list: r.automations}
	}
	p := NewPipeline(d, NewRegistry(zap.NewNop(), mods...), Config{OutgressPremium: premiumSubj, OutgressStandard: standardSubj})
	hook.Wire(p)
	p.autoIntN = func(int) int { return r.intN }
	return p, runs
}

func rawJSON(v any) codec.RawMessage {
	b, err := codec.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}

func cond(typ string, params map[string]any) automation.Condition {
	return automation.Condition{Type: typ, Params: rawJSON(params)}
}

func testAutomation(name, trigger string, params map[string]any, conds ...automation.Condition) automation.Automation {
	return automation.Automation{
		ID: "id-" + name, UserID: 123, Name: name, Enabled: true, Revision: 1,
		Definition: automation.Definition{
			Version:    automation.Version,
			Trigger:    automation.Trigger{Type: trigger, Params: rawJSON(params)},
			Conditions: conds,
		},
	}
}

func command(name string, params map[string]any, conds ...automation.Condition) automation.Automation {
	if params["name"] == nil {
		params["name"] = name
	}
	return testAutomation(name, "command", params, conds...)
}

func process(t *testing.T, p *Pipeline, env lane.Envelope) {
	t.Helper()
	body, err := codec.Marshal(env)
	require.NoError(t, err)
	require.NoError(t, p.Process(bus.NewMessage("uuid-auto", body)))
}

func eventEnv(kind, event string) lane.Envelope {
	return lane.Envelope{Type: kind, Lane: "standard", EventID: "ev-1", BroadcasterUserID: "123", Event: codec.RawMessage(event)}
}

func TestProcessAutomationCommandRouting(t *testing.T) {
	custom := projection.Command{Name: "hi", Response: "custom", IsActive: true, Perm: "everyone"}
	hi := func(params map[string]any, conds ...automation.Condition) automation.Automation {
		return command("!hi", params, conds...)
	}
	cases := []struct {
		name string
		rig  autoRig
		text string
		want []string
	}{
		{"runs when unclaimed", autoRig{automations: []automation.Automation{hi(map[string]any{})}}, "!hi", []string{"!hi:ok"}},
		{"built-in wins", autoRig{builtin: true, automations: []automation.Automation{hi(map[string]any{})}}, "!hi", nil},
		{"active custom wins", autoRig{cmd: custom, cmdFound: true, automations: []automation.Automation{hi(map[string]any{})}}, "!hi", nil},
		{"inactive custom falls through", autoRig{cmd: projection.Command{Name: "hi", Response: "x"}, cmdFound: true, automations: []automation.Automation{hi(map[string]any{})}}, "!hi", []string{"!hi:ok"}},
		{"start accepts args", autoRig{automations: []automation.Automation{hi(map[string]any{})}}, "!hi there", []string{"!hi:ok"}},
		{"exact rejects args", autoRig{automations: []automation.Automation{hi(map[string]any{"match": "exact"})}}, "!hi there", nil},
		{"exact accepts bare name", autoRig{automations: []automation.Automation{hi(map[string]any{"match": "exact"})}}, "!hi", []string{"!hi:ok"}},
		{"alias", autoRig{automations: []automation.Automation{hi(map[string]any{"aliases": []string{"!hello"}})}}, "!hello", []string{"!hi:ok"}},
		{"case insensitive", autoRig{automations: []automation.Automation{hi(map[string]any{})}}, "!HI", []string{"!hi:ok"}},
		{"other name", autoRig{automations: []automation.Automation{hi(map[string]any{})}}, "!bye", nil},
		{"args reach conditions", autoRig{automations: []automation.Automation{hi(map[string]any{}, cond("arg_regex", map[string]any{"pattern": "^abc$"}))}}, "!hi abc", []string{"!hi:ok"}},
		{"args fail conditions", autoRig{automations: []automation.Automation{hi(map[string]any{}, cond("arg_regex", map[string]any{"pattern": "^abc$"}))}}, "!hi xyz", []string{"!hi:skipped:condition:0:arg_regex"}},
		{"orders by name", autoRig{automations: []automation.Automation{
			command("!b", map[string]any{"name": "!hi"}), command("!a", map[string]any{"name": "!hi"}),
		}}, "!hi", []string{"!a:ok", "!b:ok"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p, runs := tc.rig.pipeline()
			process(t, p, chatEnv(tc.text, ""))
			assert.Equal(t, tc.want, runs.summary())
		})
	}
}

func TestProcessAutomationTextTriggers(t *testing.T) {
	cases := []struct {
		name   string
		params map[string]any
		text   string
		want   []string
	}{
		{"start match", map[string]any{"name": "hello"}, "hello world", []string{"t:ok"}},
		{"start needs word boundary", map[string]any{"name": "hello"}, "helloworld", nil},
		{"start folds case", map[string]any{"name": "hello"}, "HeLLo", []string{"t:ok"}},
		{"start case sensitive", map[string]any{"name": "hello", "case_sensitive": true}, "Hello", nil},
		{"start prefix routed command with space", map[string]any{"name": "!hi"}, "!hi", []string{"t:ok"}},
		{"exact", map[string]any{"name": "hello", "match": "exact"}, "  hello ", []string{"t:ok"}},
		{"exact rejects extra", map[string]any{"name": "hello", "match": "exact"}, "hello you", nil},
		{"anywhere", map[string]any{"name": "bagel", "match": "anywhere"}, "I love a Bagel today", []string{"t:ok"}},
		{"anywhere case sensitive", map[string]any{"name": "bagel", "match": "anywhere", "case_sensitive": true}, "I love a Bagel", nil},
		{"regex", map[string]any{"name": `^b[ae]gel\d+$`, "match": "regex"}, "bagel42", []string{"t:ok"}},
		{"regex folds case", map[string]any{"name": `^bagel$`, "match": "regex"}, "BAGEL", []string{"t:ok"}},
		{"regex case sensitive", map[string]any{"name": `^bagel$`, "match": "regex", "case_sensitive": true}, "BAGEL", nil},
		{"invalid regex never matches", map[string]any{"name": `(`, "match": "regex"}, "(", nil},
		{"alias on text trigger", map[string]any{"name": "hello", "aliases": []string{"howdy"}}, "howdy all", []string{"t:ok"}},
		{"sub_message only ignores chat", map[string]any{"name": "hello", "sources": []string{"sub_message"}}, "hello", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rig := autoRig{automations: []automation.Automation{testAutomation("t", "command", tc.params)}}
			p, runs := rig.pipeline()
			process(t, p, chatEnv(tc.text, ""))
			assert.Equal(t, tc.want, runs.summary())
		})
	}
}

func TestProcessAutomationSubMessageSource(t *testing.T) {
	event := `{"user_id":"7","user_login":"bob","user_name":"Bob","message":{"text":"hype train"}}`
	cases := []struct {
		name    string
		sources []string
		want    []string
	}{
		{"enabled", []string{"sub_message"}, []string{"t:ok"}},
		{"not listed", []string{"chat"}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := testAutomation("t", "command", map[string]any{"name": "hype", "sources": tc.sources})
			p, runs := autoRig{automations: []automation.Automation{a}}.pipeline()
			process(t, p, eventEnv("channel.subscription.message", event))
			assert.Equal(t, tc.want, runs.summary())
		})
	}
}

func TestProcessAutomationEventTriggers(t *testing.T) {
	cheer := `{"user_id":"7","user_login":"bob","user_name":"Bob","bits":150,"message":"hello"}`
	redeem := `{"user_id":"7","user_login":"bob","user_name":"Bob","reward":{"id":"r-1","title":"Hydrate","cost":100}}`
	cases := []struct {
		name  string
		typ   string
		event string
		trig  string
		param map[string]any
		want  []string
	}{
		{"kind match", "channel.cheer", cheer, "event", map[string]any{"kind": "channel.cheer"}, []string{"t:ok"}},
		{"other kind", "channel.follow", cheer, "event", map[string]any{"kind": "channel.cheer"}, nil},
		{"numeric filter met", "channel.cheer", cheer, "event", map[string]any{"kind": "channel.cheer", "filters": map[string]string{"amount": "100"}}, []string{"t:ok"}},
		{"numeric filter exact", "channel.cheer", cheer, "event", map[string]any{"kind": "channel.cheer", "filters": map[string]string{"amount": "150"}}, []string{"t:ok"}},
		{"numeric filter unmet", "channel.cheer", cheer, "event", map[string]any{"kind": "channel.cheer", "filters": map[string]string{"amount": "151"}}, nil},
		{"string filter folds case", "channel.cheer", cheer, "event", map[string]any{"kind": "channel.cheer", "filters": map[string]string{"login": "BOB"}}, []string{"t:ok"}},
		{"string filter unmet", "channel.cheer", cheer, "event", map[string]any{"kind": "channel.cheer", "filters": map[string]string{"login": "alice"}}, nil},
		{"reward id", "channel.channel_points_custom_reward_redemption.add", redeem, "reward", map[string]any{"reward_id": "r-1"}, []string{"t:ok"}},
		{"reward other id", "channel.channel_points_custom_reward_redemption.add", redeem, "reward", map[string]any{"reward_id": "r-2"}, nil},
		{"reward ignores other events", "channel.cheer", cheer, "reward", map[string]any{"reward_id": "r-1"}, nil},
		{"timer never matches", "channel.cheer", cheer, "timer", map[string]any{"interval_s": 60}, nil},
		{"manual never matches", "channel.cheer", cheer, "manual", map[string]any{}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p, runs := autoRig{automations: []automation.Automation{testAutomation("t", tc.trig, tc.param)}}.pipeline()
			process(t, p, eventEnv(tc.typ, tc.event))
			assert.Equal(t, tc.want, runs.summary())
		})
	}
}

func TestProcessAutomationGates(t *testing.T) {
	hi := command("!hi", map[string]any{})
	disabled := hi
	disabled.Enabled = false
	cases := []struct {
		name string
		rig  autoRig
		mut  func(*lane.Envelope)
		want []string
	}{
		{"disabled automation", autoRig{automations: []automation.Automation{disabled}}, nil, nil},
		{"module disabled", autoRig{automations: []automation.Automation{hi}, modules: map[string]projection.ModuleView{AutomationsModuleName: off()}}, nil, nil},
		{"module absent", autoRig{automations: []automation.Automation{hi}, modules: map[string]projection.ModuleView{}}, nil, nil},
		{"trial origin", autoRig{automations: []automation.Automation{hi}}, func(e *lane.Envelope) { e.Origin = "trial" }, nil},
		{"no source", autoRig{automations: []automation.Automation{hi}, nilSource: true}, nil, nil},
		{"enabled", autoRig{automations: []automation.Automation{hi}}, nil, []string{"!hi:ok"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p, runs := tc.rig.pipeline()
			env := chatEnv("!hi", "")
			if tc.mut != nil {
				tc.mut(&env)
			}
			process(t, p, env)
			assert.Equal(t, tc.want, runs.summary())
		})
	}
}

func TestProcessAutomationConditions(t *testing.T) {
	cases := []struct {
		name  string
		rig   autoRig
		badge string
		cond  automation.Condition
		want  string
	}{
		{"perm met", autoRig{}, "moderator", cond("perm", map[string]any{"level": "mod"}), "t:ok"},
		{"perm unmet", autoRig{}, "", cond("perm", map[string]any{"level": "mod"}), "t:skipped:condition:0:perm"},
		{"users allow met", autoRig{}, "", cond("users", map[string]any{"allow": []string{"ALICE"}}), "t:ok"},
		{"users allow unmet", autoRig{}, "", cond("users", map[string]any{"allow": []string{"bob"}}), "t:skipped:condition:0:users"},
		{"users deny", autoRig{}, "", cond("users", map[string]any{"deny": []string{"alice"}}), "t:skipped:condition:0:users"},
		{"live met", autoRig{}, "", cond("live", map[string]any{"state": "live"}), "t:ok"},
		{"live unmet", autoRig{}, "", cond("live", map[string]any{"state": "offline"}), "t:skipped:condition:0:live"},
		{"chance met", autoRig{intN: 9}, "", cond("chance", map[string]any{"percent": 10}), "t:ok"},
		{"chance unmet", autoRig{intN: 10}, "", cond("chance", map[string]any{"percent": 10}), "t:skipped:condition:0:chance"},
		{"counter met", autoRig{loyalty: &stubLoyalty{counters: map[string]int64{"c": 5}}}, "", cond("counter", map[string]any{"name": "c", "op": "gte", "value": 5}), "t:ok"},
		{"counter unmet", autoRig{loyalty: &stubLoyalty{counters: map[string]int64{"c": 5}}}, "", cond("counter", map[string]any{"name": "c", "op": "gt", "value": 5}), "t:skipped:condition:0:counter"},
		{"counter without store", autoRig{}, "", cond("counter", map[string]any{"name": "c", "op": "eq", "value": 0}), "t:skipped:condition:0:counter"},
		{"points met", autoRig{loyalty: &stubLoyalty{balances: map[uint64]loyaltyrpc.Balance{999: {Points: 10}}}}, "", cond("points", map[string]any{"op": "gte", "value": 10}), "t:ok"},
		{"points unmet", autoRig{loyalty: &stubLoyalty{balances: map[uint64]loyaltyrpc.Balance{999: {Points: 9}}}}, "", cond("points", map[string]any{"op": "gte", "value": 10}), "t:skipped:condition:0:points"},
		{"category allow", autoRig{stream: &stubStreamInfo{byAddress: map[string]StreamInfoResult{"123/": liveNow()}}}, "", cond("category", map[string]any{"allow": []string{"just chatting"}}), "t:ok"},
		{"category deny", autoRig{stream: &stubStreamInfo{byAddress: map[string]StreamInfoResult{"123/": liveNow()}}}, "", cond("category", map[string]any{"deny": []string{"Just Chatting"}}), "t:skipped:condition:0:category"},
		{"category without lookup", autoRig{}, "", cond("category", map[string]any{"allow": []string{"x"}}), "t:skipped:condition:0:category"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tc.rig.automations = []automation.Automation{testAutomation("t", "command", map[string]any{"name": "!t"}, tc.cond)}
			p, runs := tc.rig.pipeline()
			process(t, p, chatEnv("!t", tc.badge))
			assert.Equal(t, []string{tc.want}, runs.summary())
		})
	}
}

func TestProcessAutomationUsersIDs(t *testing.T) {
	anonymous := func(env *lane.Envelope) { env.ChatterUserID = "" }
	cases := []struct {
		name   string
		params map[string]any
		mut    func(*lane.Envelope)
		want   string
	}{
		{"allow_ids match", map[string]any{"allow_ids": []string{"999"}}, nil, "t:ok"},
		{"allow_ids miss", map[string]any{"allow_ids": []string{"1"}}, nil, "t:skipped:condition:0:users"},
		{"deny_ids hit", map[string]any{"deny_ids": []string{"999"}}, nil, "t:skipped:condition:0:users"},
		{"deny_ids miss", map[string]any{"deny_ids": []string{"1"}}, nil, "t:ok"},
		{"deny_ids beats allow_ids", map[string]any{"allow_ids": []string{"999"}, "deny_ids": []string{"999"}}, nil, "t:skipped:condition:0:users"},
		{"deny login beats allow_ids", map[string]any{"allow_ids": []string{"999"}, "deny": []string{"alice"}}, nil, "t:skipped:condition:0:users"},
		{"allow login or allow_ids: login", map[string]any{"allow": []string{"alice"}, "allow_ids": []string{"1"}}, nil, "t:ok"},
		{"allow login or allow_ids: id", map[string]any{"allow": []string{"bob"}, "allow_ids": []string{"999"}}, nil, "t:ok"},
		{"allow login or allow_ids: neither", map[string]any{"allow": []string{"bob"}, "allow_ids": []string{"1"}}, nil, "t:skipped:condition:0:users"},
		{"viewer id 0 never matches allow_ids", map[string]any{"allow_ids": []string{"0", "999"}}, anonymous, "t:skipped:condition:0:users"},
		{"viewer id 0 never matches deny_ids", map[string]any{"deny_ids": []string{"0"}}, anonymous, "t:ok"},
		{"logins only allow", map[string]any{"allow": []string{"alice"}, "deny": []string{"bob"}}, nil, "t:ok"},
		{"logins only deny", map[string]any{"allow": []string{"alice"}, "deny": []string{"ALICE"}}, nil, "t:skipped:condition:0:users"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rig := autoRig{automations: []automation.Automation{testAutomation("t", "command", map[string]any{"name": "!t"}, cond("users", tc.params))}}
			p, runs := rig.pipeline()
			env := chatEnv("!t", "")
			if tc.mut != nil {
				tc.mut(&env)
			}
			process(t, p, env)
			assert.Equal(t, []string{tc.want}, runs.summary())
		})
	}
}

func TestConditionUsersDoesNotAllocate(t *testing.T) {
	run := &AutomationRun{Viewer: Viewer{ID: 999, Login: "alice"}}
	c := automation.UsersCondition{
		Allow: []string{"bob"}, AllowIDs: []string{"999"}, AllowUserIDs: []uint64{1, 999}, DenyUserIDs: []uint64{2},
	}
	var p *Pipeline
	ok, _ := p.conditionUsers(context.Background(), run, c)
	require.True(t, ok)
	assert.Zero(t, testing.AllocsPerRun(1000, func() { _, _ = p.conditionUsers(context.Background(), run, c) }))
}

func TestProcessAutomationCooldownClaimsLast(t *testing.T) {
	cooldown := cond("cooldown", map[string]any{"global_s": 30, "user_s": 10})
	chance := cond("chance", map[string]any{"percent": 10})
	cases := []struct {
		name     string
		intN     int
		deny     bool
		conds    []automation.Condition
		want     string
		wantKeys []string
	}{
		{"earlier failure never claims", 50, false, []automation.Condition{cooldown, chance}, "t:skipped:condition:1:chance", nil},
		{"claims both windows after others pass", 0, false, []automation.Condition{cooldown, chance}, "t:ok", []string{"cooldown:auto:123:id-t", "cooldown:autouser:123:999:id-t"}},
		{"denied claim", 0, true, []automation.Condition{chance, cooldown}, "t:skipped:condition:1:cooldown", []string{"cooldown:auto:123:id-t", "cooldown:autouser:123:999:id-t"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := &recordingCooldown{deny: tc.deny}
			a := testAutomation("t", "command", map[string]any{"name": "!t"}, tc.conds...)
			p, runs := autoRig{automations: []automation.Automation{a}, cooldown: store, intN: tc.intN}.pipeline()
			process(t, p, chatEnv("!t", ""))
			assert.Equal(t, []string{tc.want}, runs.summary())
			assert.Equal(t, tc.wantKeys, store.keys)
		})
	}
}

func TestProcessAutomationEventRunsCountViewerAsEveryone(t *testing.T) {
	a := testAutomation("t", "event", map[string]any{"kind": "channel.follow"}, cond("perm", map[string]any{"level": "mod"}))
	p, runs := autoRig{automations: []automation.Automation{a}}.pipeline()
	process(t, p, eventEnv("channel.follow", `{"user_id":"7","user_login":"bob","user_name":"Bob"}`))
	assert.Equal(t, []string{"t:skipped:condition:0:perm"}, runs.summary())
}

func TestRunNamedCountsRPCViewerAsBroadcaster(t *testing.T) {
	cases := []struct {
		level string
		want  string
	}{
		{"broadcaster", "t:ok"},
		{"mod", "t:ok"},
	}
	for _, tc := range cases {
		t.Run(tc.level, func(t *testing.T) {
			a := testAutomation("t", "manual", map[string]any{}, cond("perm", map[string]any{"level": tc.level}))
			p, runs := autoRig{automations: []automation.Automation{a}}.pipeline()
			reply := p.RunNamed(context.Background(), automationsrpc.RunRequest{BroadcasterID: "123", Name: "t", Source: "api", RequestID: "req-1"})
			require.True(t, reply.Accepted)
			assert.Equal(t, []string{tc.want}, runs.summary())
		})
	}
}
