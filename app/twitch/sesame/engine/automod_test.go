// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"cmp"
	"context"
	"strconv"
	"sync"
	"testing"

	"ItsBagelBot/app/twitch/sesame/automod"
	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/internal/domain/outgress"
	"ItsBagelBot/internal/projection"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/codec"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

const (
	raidLink     = "everyone hurry claim your prize at grabify.link/xyz right now"
	linkFloodMsg = "hey friends come check this great video at https://example.com/watch tonight"
	vbucksMsg    = "FREE VBUCKS CLICK MY PROFILE RIGHT NOW EVERYONE HURRY"
)

type fakeCampaign struct {
	mu    sync.Mutex
	count int
	calls int
	seen  []campaignCall
}

type campaignCall struct {
	broadcasterID uint64
	simhash       uint64
	sender        string
}

func (c *fakeCampaign) Observe(_ context.Context, broadcasterID uint64, simhash uint64, senderID string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls++
	c.seen = append(c.seen, campaignCall{broadcasterID: broadcasterID, simhash: simhash, sender: senderID})
	return c.count
}

type fakeRep struct {
	mu     sync.Mutex
	bumps  map[string]int
	scores map[string]int
}

func (r *fakeRep) Bump(_ context.Context, id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.bumps == nil {
		r.bumps = map[string]int{}
	}
	r.bumps[id]++
}

func (r *fakeRep) Score(_ context.Context, id string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.scores[id]
}

type automodRig struct {
	reader   fakeReader
	module   *module.Module
	shadow   bool
	shield   bool
	adaptive bool
	campaign int
	scores   map[string]int
	rep      bool
	stats    bool
}

type automodRun struct {
	stats bool
	p     *Pipeline
	pub   *fakePublisher
	camp  *fakeCampaign
	rep   *fakeRep
	logs  *observer.ObservedLogs
}

func (r automodRig) build() automodRun {
	run := automodRun{pub: &fakePublisher{}}
	gate := automod.New()
	gate.SetEmotes(automod.NewEmoteSet(nil))
	core, logs := observer.New(zapcore.DebugLevel)
	run.logs = logs
	d := Deps{
		Proj: r.reader, Live: liveAlways{}, Cooldown: NoopCooldown{},
		Pub: run.pub, Log: zap.New(core), Automod: gate,
	}
	if r.campaign > 0 {
		run.camp = &fakeCampaign{count: r.campaign}
		d.Campaign = run.camp
	}
	if r.rep {
		run.rep = &fakeRep{scores: r.scores}
		d.Reputation = run.rep
	}
	if r.stats {
		d.Stats = &fakeBumper{}
		run.stats = true
	}
	var mods []module.Module
	if r.module != nil {
		mods = append(mods, *r.module)
	}
	run.p = NewPipeline(d, NewRegistry(zap.NewNop(), mods...), Config{
		OutgressPremium: premiumSubj, OutgressStandard: standardSubj,
		AutomodEnforce: !r.shadow, ShieldEnabled: r.shield, AdaptiveEnabled: r.adaptive,
	})
	return run
}

type automodWant struct {
	types         map[string]int
	deleted       []string
	bumps         map[string]int
	campaignCalls int
	audit         string
	flags         map[string]int64
	flagChannels  map[uint64][2]int64
}

func (run automodRun) outcome() automodWant {
	got := automodWant{types: run.pub.types()}
	for _, c := range run.pub.snapshot() {
		if c.msg.Type == outgress.TypeDelete {
			got.deleted = append(got.deleted, c.msg.MsgID)
		}
	}
	if run.rep != nil {
		got.bumps = run.rep.bumps
	}
	if run.camp != nil {
		got.campaignCalls = run.camp.calls
	}
	for _, entry := range run.logs.FilterMessage("campaign band quorum").All() {
		got.audit = entry.ContextMap()["chatter_id"].(string)
	}
	if run.stats {
		run.p.Close()
		got.flags, got.flagChannels = loggedFlagFields(run.logs)
	}
	return got
}

type automodMsg struct {
	lane    string
	channel string
	text    string
	chatter string
	msgID   string
	cohort  int
	senders []string
	emotes  int
}

func (m automodMsg) emoteSpans() []map[string]any {
	emotes := make([]map[string]any, m.emotes)
	for i := range emotes {
		emotes[i] = map[string]any{"id": "425618", "begin": i * 4, "end": i*4 + 3}
	}
	return emotes
}

func (m automodMsg) message(t *testing.T) *bus.Message {
	t.Helper()
	extra := map[string]any{}
	if m.lane != "" {
		extra["lane"] = m.lane
	}
	if m.channel != "" {
		extra["broadcaster_user_id"] = m.channel
	}
	if m.msgID != "" {
		extra["msg_id"] = m.msgID
	}
	if m.emotes > 0 {
		extra["emotes"] = m.emoteSpans()
	}
	switch {
	case m.cohort > 0:
		return cohortMsg(t, m.cohort, m.text, extra)
	case m.senders != nil:
		senders := make([]map[string]any, len(m.senders))
		for i, id := range m.senders {
			senders[i] = map[string]any{"chatter_user_id": id}
		}
		extra["senders"] = senders
	default:
		extra["chatter_user_id"] = cmp.Or(m.chatter, "999")
	}
	extra["text"] = m.text
	return envelopeMsg(t, "u", extra)
}

type automodCase struct {
	name string
	rig  automodRig
	msgs []automodMsg
	want automodWant
}

func automodModule(beta bool) *module.Module {
	m := module.NewModule("automod", module.KindDefault)
	if beta {
		m.Beta()
	}
	m.On(chatType, func(context.Context, *module.Context, module.Emit) error { return nil })
	built := m.Build()
	return &built
}

func automodView(enabled bool, configs string) fakeReader {
	return fakeReader{modules: projection.ModuleMap([]projection.ModuleView{
		{Name: "automod", IsEnabled: enabled, Configs: codec.RawMessage(configs)},
	})}
}

var (
	ipLogger = automodMsg{text: "claim your prize over at grabify.link/xyz now everyone hurry"}
	hypeLine = automodMsg{text: "LUL LUL LUL LUL", msgID: "m-999", emotes: 4}

	harassment = automodMsg{text: "nobody asked just go kill yourself already dude seriously", msgID: "m-999"}
)

func linkFlood(chatter string) automodMsg {
	return automodMsg{text: linkFloodMsg, chatter: chatter, msgID: "m-" + chatter}
}

func TestAutomodActsOnVerdicts(t *testing.T) {
	var cases []automodCase
	for _, family := range [][]automodCase{enforcementCases(), moduleConfigCases(), campaignCases(), massRaidCases(), reputationCases(), flagCountCases()} {
		cases = append(cases, family...)
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			run := tc.rig.build()
			for _, m := range tc.msgs {
				require.NoError(t, run.p.Process(m.message(t)))
			}

			assert.Equal(t, tc.want, run.outcome())
		})
	}
}

func enforcementCases() []automodCase {
	command := fakeReader{cmd: projection.Command{Name: "x", Response: "hi", IsActive: true}, cmdFound: true}
	timeout := map[string]int{outgress.TypeTimeout: 1}
	return []automodCase{
		{
			name: "enforcement emits one timeout for an ip logger",
			msgs: []automodMsg{ipLogger},
			want: automodWant{types: timeout},
		},
		{
			name: "enforcement skips command dispatch",
			rig:  automodRig{reader: command},
			msgs: []automodMsg{ipLogger},
			want: automodWant{types: timeout},
		},
		{
			name: "shadow mode emits nothing",
			rig:  automodRig{shadow: true},
			msgs: []automodMsg{ipLogger},
		},
		{
			name: "adaptive off still deletes caps-only hype",
			msgs: []automodMsg{hypeLine},
			want: automodWant{types: map[string]int{outgress.TypeDelete: 1}, deleted: []string{"m-999"}},
		},
		{
			name: "adaptive on rescues span-covered native emotes",
			rig:  automodRig{adaptive: true},
			msgs: []automodMsg{hypeLine},
		},
	}
}

func moduleConfigCases() []automodCase {
	timeout := map[string]int{outgress.TypeTimeout: 1}
	harassed := automodWant{types: map[string]int{outgress.TypeWarn: 1, outgress.TypeDelete: 1}, deleted: []string{"m-999"}}
	moderate := automodView(true, `{"level":"moderate"}`)
	return []automodCase{
		{
			name: "a disabled module row still holds the floor",
			rig:  automodRig{reader: automodView(false, ""), module: automodModule(false)},
			msgs: []automodMsg{ipLogger},
			want: automodWant{types: timeout},
		},
		{
			name: "a disabled module row turns the non-floor checks off",
			rig:  automodRig{reader: automodView(false, ""), module: automodModule(false)},
			msgs: []automodMsg{harassment},
		},
		{
			name: "an absent module row means enabled by default",
			rig:  automodRig{module: automodModule(false)},
			msgs: []automodMsg{ipLogger, harassment},
			want: automodWant{types: map[string]int{outgress.TypeTimeout: 1, outgress.TypeWarn: 1, outgress.TypeDelete: 1}, deleted: []string{"m-999"}},
		},
		{
			name: "an enabled module row acts on the floor",
			rig:  automodRig{reader: moderate, module: automodModule(false)},
			msgs: []automodMsg{ipLogger},
			want: automodWant{types: timeout},
		},
		{
			name: "the configured level reaches the gate and can switch the checks off",
			rig:  automodRig{reader: automodView(true, `{"level":"none"}`), module: automodModule(false)},
			msgs: []automodMsg{harassment},
		},
		{
			name: "the adult profile keeps the harassment check",
			rig:  automodRig{reader: automodView(true, `{"profile":"adult"}`), module: automodModule(false)},
			msgs: []automodMsg{harassment},
			want: harassed,
		},
		{
			name: "the adult profile keeps the floor",
			rig:  automodRig{reader: automodView(true, `{"profile":"adult"}`), module: automodModule(false)},
			msgs: []automodMsg{ipLogger},
			want: automodWant{types: timeout},
		},
		{
			name: "beta automod is floor-only on the standard lane",
			rig:  automodRig{reader: moderate, module: automodModule(true)},
			msgs: []automodMsg{harassment},
		},
		{
			name: "the floor still holds on a locked channel",
			rig:  automodRig{reader: moderate, module: automodModule(true)},
			msgs: []automodMsg{ipLogger},
			want: automodWant{types: timeout},
		},
		{
			name: "beta automod runs every check on the premium lane",
			rig:  automodRig{reader: moderate, module: automodModule(true)},
			msgs: []automodMsg{{text: harassment.text, msgID: "m-999", lane: "premium"}},
			want: harassed,
		},
		{
			name: "a locked standard lane does not poison the premium lane's config",
			rig:  automodRig{reader: moderate, module: automodModule(true)},
			msgs: []automodMsg{harassment, {text: harassment.text, msgID: "m-999", lane: "premium"}},
			want: harassed,
		},
	}
}

func campaignCases() []automodCase {
	vbucks := automodMsg{text: vbucksMsg, chatter: "888", msgID: "m-888"}
	backed := automodMsg{text: vbucksMsg + " bit.ly/free-vbucks", chatter: "888", msgID: "m-888"}
	ordinary := make([]automodMsg, 5)
	for i := range ordinary {
		ordinary[i] = automodMsg{text: "that jungle gank was so clean honestly round " + string(rune('0'+i))}
	}
	return []automodCase{
		{
			name: "campaign corroboration adds the mildest action to a link flood",
			rig:  automodRig{campaign: campaignThreshold},
			msgs: []automodMsg{linkFlood("777")},
			want: automodWant{types: map[string]int{outgress.TypeDelete: 1}, deleted: []string{"m-777"}, campaignCalls: 1, audit: "777"},
		},
		{
			name: "below the quorum the campaign juror abstains but the line was counted",
			rig:  automodRig{campaign: campaignThreshold - 1},
			msgs: []automodMsg{linkFlood("777")},
			want: automodWant{campaignCalls: 1},
		},
		{
			name: "a clean short line never reaches the campaign juror",
			rig:  automodRig{campaign: 100},
			msgs: []automodMsg{{text: "nice play"}},
		},
		{
			name: "ordinary chatter never counts",
			rig:  automodRig{campaign: 100},
			msgs: ordinary,
		},
		{
			name: "a flagged delete plus campaign quorum escalates to a timeout",
			rig:  automodRig{campaign: campaignThreshold},
			msgs: []automodMsg{vbucks},
			want: automodWant{types: map[string]int{outgress.TypeTimeout: 1}, campaignCalls: 1, audit: "888"},
		},
		{
			name: "harassment issues a formal warning and removes the message",
			msgs: []automodMsg{harassment},
			want: automodWant{types: map[string]int{outgress.TypeWarn: 1, outgress.TypeDelete: 1}, deleted: []string{"m-999"}},
		},
		{
			name: "TestCampaignOnlyVerdictSkipsReputationStrikeButAudits",
			rig:  automodRig{campaign: campaignThreshold, rep: true},
			msgs: []automodMsg{linkFlood("777")},
			want: automodWant{
				types: map[string]int{outgress.TypeDelete: 1}, deleted: []string{"m-777"}, campaignCalls: 1, audit: "777",
			},
		},
		{
			name: "TestContentBackedCampaignEscalationStillScores",
			rig:  automodRig{campaign: campaignThreshold, rep: true},
			msgs: []automodMsg{backed},
			want: automodWant{types: map[string]int{outgress.TypeTimeout: 1}, bumps: map[string]int{"888": 1}, campaignCalls: 1, audit: "888"},
		},
	}
}

func massRaidCases() []automodCase {
	shield := automodRig{shield: true}
	return []automodCase{
		{
			name: "a mass raid escalates to shield mode and bans the capped prefix",
			rig:  shield,
			msgs: []automodMsg{{text: raidLink, cohort: massRaidBanCap + 30}},
			want: automodWant{types: map[string]int{outgress.TypeShieldMode: 1, outgress.TypeTimeout: massRaidBanCap}},
		},
		{
			name: "a small hostile cohort bans every member and never trips shield mode",
			rig:  shield,
			msgs: []automodMsg{{text: raidLink, cohort: massRaidThreshold - 1}},
			want: automodWant{types: map[string]int{outgress.TypeTimeout: massRaidThreshold - 1}},
		},
		{
			name: "shield disabled still bans the cohort within the cap",
			msgs: []automodMsg{{text: raidLink, cohort: massRaidThreshold + 5}},
			want: automodWant{types: map[string]int{outgress.TypeTimeout: massRaidThreshold + 5}},
		},
		{
			name: "a clean cohort is never actioned",
			rig:  shield,
			msgs: []automodMsg{{text: "PogChamp what a play", cohort: massRaidThreshold + 10}},
		},
		{
			name: "shadow mode emits nothing even for a mass raid",
			rig:  automodRig{shield: true, shadow: true},
			msgs: []automodMsg{{text: raidLink, cohort: massRaidThreshold + 10}},
		},
		{
			name: "repeated folds activate shield mode once per cooldown",
			rig:  shield,
			msgs: []automodMsg{{text: raidLink, cohort: massRaidThreshold + 1}, {text: raidLink, cohort: massRaidThreshold + 1}},
			want: automodWant{types: map[string]int{outgress.TypeShieldMode: 1, outgress.TypeTimeout: 2 * (massRaidThreshold + 1)}},
		},
		{
			name: "each channel has its own shield cooldown",
			rig:  shield,
			msgs: []automodMsg{
				{text: raidLink, cohort: massRaidThreshold + 1},
				{text: raidLink, cohort: massRaidThreshold + 1, channel: "456"},
			},
			want: automodWant{types: map[string]int{outgress.TypeShieldMode: 2, outgress.TypeTimeout: 2 * (massRaidThreshold + 1)}},
		},
		{
			name: "a mass cohort with only a delete verdict never trips shield mode",
			rig:  shield,
			msgs: []automodMsg{{text: hypeLine.text, emotes: 4, cohort: massRaidThreshold + 5}},
		},
		{
			name: "a hostile cohort carrying a command never gets a chat reply",
			rig: automodRig{
				reader: fakeReader{cmd: projection.Command{Name: "x", Response: "hi", IsActive: true}, cmdFound: true},
				shield: true,
			},
			msgs: []automodMsg{{text: raidLink, cohort: 3}},
			want: automodWant{types: map[string]int{outgress.TypeTimeout: 3}},
		},
	}
}

func reputationCases() []automodCase {
	strikes := map[string]int{"1": 1, "2": 1, "3": 1}
	return []automodCase{
		{
			name: "a cohort fans out reputation per sender",
			rig:  automodRig{rep: true},
			msgs: []automodMsg{{text: raidLink, senders: []string{"a", "b", "a"}}},
			want: automodWant{types: map[string]int{outgress.TypeTimeout: 3}, bumps: map[string]int{"a": 2, "b": 1}},
		},
		{
			name: "a shadow hostile fold records no strike",
			rig:  automodRig{rep: true, shadow: true},
			msgs: []automodMsg{{text: raidLink, cohort: 3}},
		},
		{
			name: "a benign fold under enforcement records no strike",
			rig:  automodRig{rep: true},
			msgs: []automodMsg{{text: "PogChamp what a play", cohort: 3}},
		},
		{
			name: "a hostile fold under enforcement strikes every member",
			rig:  automodRig{rep: true},
			msgs: []automodMsg{{text: raidLink, cohort: 3}},
			want: automodWant{types: map[string]int{outgress.TypeTimeout: 3}, bumps: strikes},
		},
		{
			name: "a shadow single chatter records no strike",
			rig:  automodRig{rep: true, shadow: true},
			msgs: []automodMsg{ipLogger},
		},
		{
			name: "a repeat offender's timeout escalates to a ban",
			rig:  automodRig{rep: true, scores: map[string]int{"999": repEscalateThreshold + 2}},
			msgs: []automodMsg{ipLogger},
			want: automodWant{types: map[string]int{outgress.TypeBan: 1}, bumps: map[string]int{"999": 1}},
		},
		{
			name: "a timeout below the ban threshold stays a timeout",
			rig:  automodRig{rep: true, scores: map[string]int{"999": repEscalateThreshold - 1}},
			msgs: []automodMsg{ipLogger},
			want: automodWant{types: map[string]int{outgress.TypeTimeout: 1}, bumps: map[string]int{"999": 1}},
		},
		{
			name: "a repeat offender's warning escalates to a timeout",
			rig:  automodRig{rep: true, scores: map[string]int{"999": repWarnToTimeoutScore}},
			msgs: []automodMsg{harassment},
			want: automodWant{types: map[string]int{outgress.TypeTimeout: 1}, bumps: map[string]int{"999": 1}},
		},
		{
			name: "a delete never escalates however bad the reputation",
			rig:  automodRig{rep: true, scores: map[string]int{"999": 99}},
			msgs: []automodMsg{hypeLine},
			want: automodWant{types: map[string]int{outgress.TypeDelete: 1}, deleted: []string{"m-999"}, bumps: map[string]int{"999": 1}},
		},
	}
}

func flagCountCases() []automodCase {
	return []automodCase{
		{
			name: "an enforced floor verdict is counted as a flag and an enforcement",
			rig:  automodRig{stats: true},
			msgs: []automodMsg{ipLogger},
			want: automodWant{
				types:        map[string]int{outgress.TypeTimeout: 1},
				flags:        map[string]int64{"flags_total": 1, "flags_enforced": 1, "flag_rule_ip_logger": 1},
				flagChannels: map[uint64][2]int64{123: {1, 1}},
			},
		},
		{
			name: "a shadow verdict is counted as a flag but not an enforcement",
			rig:  automodRig{stats: true, shadow: true},
			msgs: []automodMsg{ipLogger},
			want: automodWant{
				flags:        map[string]int64{"flags_total": 1, "flags_enforced": 0, "flag_rule_ip_logger": 1},
				flagChannels: map[uint64][2]int64{123: {1, 0}},
			},
		},
		{
			name: "an idle window logs no flags",
			rig:  automodRig{stats: true},
			msgs: []automodMsg{{text: "nice play"}},
		},
	}
}

func TestCampaignJurorReceivesTenantScope(t *testing.T) {
	cases := []struct {
		name          string
		broadcasterID string
		chatter       string
	}{
		{name: "first channel", broadcasterID: "123", chatter: "777"},
		{name: "second channel", broadcasterID: "456", chatter: "778"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			run := automodRig{campaign: 1}.build()
			line := envelopeMsg(t, "u-"+tc.chatter, map[string]any{
				"broadcaster_user_id": tc.broadcasterID, "chatter_user_id": tc.chatter,
				"msg_id": "m-" + tc.chatter, "text": linkFloodMsg,
			})

			require.NoError(t, run.p.Process(line))

			require.NotEmpty(t, run.camp.seen, "the link-bearing line reached the juror")
			assert.Equal(t, tc.broadcasterID, strconv.FormatUint(run.camp.seen[0].broadcasterID, 10))
			assert.Equal(t, tc.chatter, run.camp.seen[0].sender)
		})
	}
}
