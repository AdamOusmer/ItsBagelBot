// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"cmp"
	"context"
	"slices"
	"testing"
	"time"

	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/internal/domain/event/lane"
	"ItsBagelBot/internal/domain/outgress"
	"ItsBagelBot/internal/projection"
	"ItsBagelBot/pkg/codec"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func customPipeline(resp, perm string) *Pipeline {
	return customCommandPipeline(projection.Command{Name: "so", Response: resp, IsActive: true, Perm: perm}, nil)
}

func customCommandPipeline(cmd projection.Command, loyalty LoyaltyStore) *Pipeline {
	d := Deps{
		Proj:     fakeReader{cmd: cmd, cmdFound: true},
		Live:     liveAlways{},
		Cooldown: NoopCooldown{},
		Pub:      &fakePublisher{},
		Loyalty:  loyalty,
		Log:      zap.NewNop(),
	}
	return NewPipeline(d, NewRegistry(zap.NewNop()), Config{OutgressPremium: premiumSubj, OutgressStandard: standardSubj})
}

type replyCase struct {
	name        string
	response    string
	line        string
	msgID       string
	chatterName string
	channelName string
	batch       bool
	want        []module.Output
}

func chatLine(text string) module.Output {
	return module.Output{Type: outgress.TypeChat, Text: text}
}

func announceLine(text string) module.Output {
	return module.Output{Type: outgress.TypeAnnounce, Color: "primary", Text: text}
}

func replyItems(got []module.Output) ([]module.Output, bool) {
	batch := len(got) == 1 && got[0].Type == outgress.TypeBatch
	if batch {
		got = got[0].Items
	}
	var items []module.Output
	for _, o := range got {
		items = append(items, module.Output{Type: o.Type, Color: o.Color, Text: o.Text})
	}
	return items, batch
}

func (tc replyCase) envelope() lane.Envelope {
	env := chatEnv(cmp.Or(tc.line, "!so"), "")
	env.MsgID = tc.msgID
	env.ChatterUserName = tc.chatterName
	env.BroadcasterUserName = tc.channelName
	return env
}

func TestCustomCommandReplies(t *testing.T) {
	cases := slices.Concat(replyEmissionCases(), replyBatchCases(), replyIdentityCases(), replyArgumentCases(), replyConditionCases())
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := replies(t, customPipeline(tc.response, "everyone"), tc.envelope())
			if tc.msgID != "" && tc.batch {
				assert.Equal(t, tc.msgID, got[0].BatchID)
			}
			items, batch := replyItems(got)
			assert.Equal(t, tc.batch, batch)
			assert.Equal(t, tc.want, items)
		})
	}
}

func replyEmissionCases() []replyCase {
	return []replyCase{
		{
			name:     "an announce slash verb routes as an announcement",
			response: "/announce {user} says: {args}; target={target}",
			line:     "!so @bob raid incoming",
			want:     []module.Output{announceLine("alice says: @bob raid incoming; target=bob")},
		},
		{
			name:     "an empty announce is skipped",
			response: "/announce",
		},
		{
			name:     "a pin slash verb routes as a pin",
			response: "/pin Current speed: {args}",
			line:     "!speed 42 km/h",
			want:     []module.Output{{Type: outgress.TypePin, Text: "Current speed: 42 km/h"}},
		},
		{
			name:     "plain text still emits chat",
			response: "hello {sender}",
			want:     []module.Output{chatLine("hello alice")},
		},
		{
			name:     "a multi-line reply emits one chat line per line in one batch",
			response: "first {user}\nsecond line\n/announce third",
			msgID:    "event-message-1",
			batch:    true,
			want:     []module.Output{chatLine("first alice"), chatLine("second line"), announceLine("third")},
		},
	}
}

func replyBatchCases() []replyCase {
	return []replyCase{
		{
			name:     "a multi-line reply is capped at five lines",
			response: "1\n2\n3\n4\n5\n6\n7",
			batch:    true,
			want:     []module.Output{chatLine("1"), chatLine("2"), chatLine("3"), chatLine("4"), chatLine("5")},
		},
		{
			name:     "blank and empty-action lines are skipped",
			response: "one\n\n/announce\ntwo",
			msgID:    "event-message-2",
			batch:    true,
			want:     []module.Output{chatLine("one"), chatLine("two")},
		},
		{
			name:     "a line emptied by a condition is dropped",
			response: "hi {user}\n{if:1: you said something}\nlast line",
			msgID:    "event-message-if",
			batch:    true,
			want:     []module.Output{chatLine("hi alice"), chatLine("last line")},
		},
		{
			name:     "a line emptied by a condition does not eat the cap",
			response: "one\n{if:1:two}\nthree\nfour\nfive",
			batch:    true,
			want:     []module.Output{chatLine("one"), chatLine("three"), chatLine("four"), chatLine("five")},
		},
		{
			name:     "a reply emptied by a condition never collapses to nothing",
			response: "{if:1:you said something}",
		},
		{
			name:     "a condition with an else branch still answers",
			response: "{if:1:you said one:you said nothing}",
			want:     []module.Output{chatLine("you said nothing")},
		},
		{name: "plain text is not a command", response: "hi", line: "hello world"},
		{name: "a bare bang is not a command", response: "hi", line: "!"},
		{
			name:     "leading spaces before the bang are tolerated",
			response: "hi",
			line:     "   !so",
			want:     []module.Output{chatLine("hi")},
		},
		{
			name:     "the command name is case-insensitive",
			response: "hi",
			line:     "!SO",
			want:     []module.Output{chatLine("hi")},
		},
		{
			name:     "arguments are trimmed",
			response: "[{args}]",
			line:     "!so    spaced   ",
			want:     []module.Output{chatLine("[spaced]")},
		},
		{
			name:     "TestCustomMultiLineSuppressionDoesNotLeaveSequenceGap",
			response: "grabify.link/bad\nsafe line",
			msgID:    "event-message-3",
			want:     []module.Output{chatLine("safe line")},
		},
	}
}

func replyIdentityCases() []replyCase {
	return []replyCase{
		{
			name:        "identity tokens use the display names",
			response:    "{channel}: {user}/{sender}",
			chatterName: "Alice",
			channelName: "StreamerName",
			want:        []module.Output{chatLine("StreamerName: Alice/Alice")},
		},
		{
			name:     "identity tokens fall back to the login",
			response: "{user}",
			want:     []module.Output{chatLine("alice")},
		},
		{
			name:        "message tokens render the sender, target and channel",
			response:    "hi {user} / {sender} -> {touser} ({target}) in {channel}",
			line:        "!so bob the rest here",
			channelName: "channel_name",
			want:        []module.Output{chatLine("hi alice / alice -> bob (bob) in channel_name")},
		},
		{
			name:     "the target is the sender when no argument is given",
			response: "{touser}",
			want:     []module.Output{chatLine("alice")},
		},
		{
			name:     "positional words and slices render from the arguments",
			response: "{args} | {1} {2} {3:} {1:2} {:2} [{9}] [{9:}]",
			line:     "!so bob the rest here",
			want:     []module.Output{chatLine("bob the rest here | bob the rest here bob the bob the [] []")},
		},
		{
			name:     "id, login and canonical command name tokens",
			response: "{user.id} {userid} {user} is {user.login} !{command}",
			line:     "!cuddle",
			want:     []module.Output{chatLine("999 999 alice is alice !so")},
		},
	}
}

func replyArgumentCases() []replyCase {
	return []replyCase{
		{
			name:     "a leading slash is defanged word by word",
			response: "{1} {touser}",
			line:     "!so /ban @everyone",
			want:     []module.Output{chatLine("ban ban")},
		},
		{
			name:     "a doubled at sign is stripped from the target",
			response: "{touser}",
			line:     "!so @@bob hi",
			want:     []module.Output{chatLine("bob")},
		},
		{
			name:     "a word that sanitizes away keeps its slot",
			response: "[{1}][{2}]",
			line:     "!so /// b",
			want:     []module.Output{chatLine("[][b]")},
		},
		{
			name:     "a viewer's slash words never mint a second command",
			response: "{1} {2} {3} {4} {5}",
			line:     "!so hey /me is a cat",
			want:     []module.Output{chatLine("hey me is a cat")},
		},
		{
			name:     "a condition on a named viewer picks the then branch",
			response: "{if:touser:hi there:hi nobody}",
			line:     "!so bob",
			want:     []module.Output{chatLine("hi there")},
		},
		{
			name:     "a condition on a missing word picks the else branch",
			response: "{if:4:word four:no fourth word}",
			want:     []module.Output{chatLine("no fourth word")},
		},
	}
}

func replyConditionCases() []replyCase {
	return []replyCase{
		{
			name:     "a condition on a missing word with no else says nothing",
			response: "[{if:4:word four}]",
			want:     []module.Output{chatLine("[]")},
		},
		{
			name:     "a condition compares against the command name case-sensitively",
			response: "{if:command=so:hugs:waves} {if:command=SO:hugs:waves}",
			want:     []module.Output{chatLine("hugs waves")},
		},
		{
			name:     "a condition keeps its own payload",
			response: "{if:2:=rest here:exact:other}",
			line:     "!so the rest here",
			want:     []module.Output{chatLine("exact")},
		},
		{
			name:     "a condition on a token this chain does not own stays literal",
			response: "{if:points:rich:poor}",
			want:     []module.Output{chatLine("{if:points:rich:poor}")},
		},
		{
			name:     "an empty value falls back and a present value wins",
			response: "shout out to {args|everyone}, hi {user|everyone}, hug {2|none}",
			want:     []module.Output{chatLine("shout out to everyone, hi alice, hug none")},
		},
		{
			name:     "an empty value with no fallback renders nothing",
			response: "hi {args}!",
			want:     []module.Output{chatLine("hi !")},
		},
		{
			name:     "a fallback never rescues an unknown or unmounted token",
			response: "{nosuchtoken|rescued} {counter:deaths|0}",
			want:     []module.Output{chatLine("{nosuchtoken|rescued} {counter:deaths|0}")},
		},
		{
			name:     "unknown tokens and unterminated braces stay literal",
			response: "keep {whatever} intact, dangling {user and {more",
			want:     []module.Output{chatLine("keep {whatever} intact, dangling {user and {more")},
		},
	}
}

func TestCustomMultiLineBatchSurvivesPublish(t *testing.T) {
	pub := &fakePublisher{}
	reader := fakeReader{
		cmd:      projection.Command{Name: "raid", Response: "line one\nline two", IsActive: true, Perm: "everyone"},
		cmdFound: true,
	}
	p := newPipelineWith(pub, reader)

	require.NoError(t, p.Process(chatMsg(t, "standard", "!raid")))
	require.Len(t, pub.got, 1)
	assert.Equal(t, outgress.TypeBatch, pub.got[0].msg.Type)
	var batch outgress.Batch
	require.NoError(t, codec.Unmarshal(pub.got[0].msg.Payload, &batch))
	assert.NotEmpty(t, batch.ID)
	require.Len(t, batch.Items, 2)
	assert.Equal(t, outgress.TypeChat, batch.Items[0].Type)
	assert.Equal(t, outgress.TypeChat, batch.Items[1].Type)
}

func TestBakedCommandReplies(t *testing.T) {
	cases := []struct {
		name  string
		reply string
		want  module.Output
	}{
		{"a baked reply runs", "pong", chatLine("pong")},
		{"a baked reply always hits the lexer", "@{user} the music lookup is down", chatLine("@alice the music lookup is down")},
		{"a baked reply expands dynamic tokens", "{choice:yes,yes} @{user}", chatLine("yes @alice")},
		{"a baked slash verb expands then routes", "/announce @{user} go", announceLine("@alice go")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := newPipelineWith(&fakePublisher{}, fakeReader{}, cmdEmit("", module.KindCore, "do", tc.reply))
			items, _ := replyItems(replies(t, p, chatEnv("!do now", "")))
			assert.Equal(t, []module.Output{tc.want}, items)
		})
	}
}

func TestBakedAndCustomShareEmitPath(t *testing.T) {
	const body = "hi {user}\n/announce {args}"
	env := chatEnv("!so raid incoming", "")
	env.MsgID = "shared-emit"

	custom := replies(t, customPipeline(body, "everyone"), env)
	baked := replies(t, newPipelineWith(&fakePublisher{}, fakeReader{}, cmdEmit("", module.KindCore, "so", body)), env)
	require.Equal(t, custom, baked)
}

func TestBakedCommandPermissions(t *testing.T) {
	cases := []struct {
		name     string
		config   string
		badge    string
		wantRuns int
	}{
		{name: "default denies viewer", wantRuns: 0},
		{name: "default allows lead moderator", badge: "lead_moderator", wantRuns: 1},
		{name: "lowered to everyone", config: `{"permission":"everyone"}`, wantRuns: 1},
		{name: "raised to broadcaster", config: `{"permission":"broadcaster"}`, badge: "lead_moderator", wantRuns: 0},
		{name: "invalid override is denied", config: `{"permission":"unknown"}`, badge: "lead_moderator", wantRuns: 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var runs int
			b := module.NewModule("", module.KindCore)
			b.Command("title").Aliases("settitle").LeadMod().Run(func(context.Context, *module.Context, string, module.Emit) error {
				runs++
				return nil
			})
			modules := map[string]projection.ModuleView{}
			if tc.config != "" {
				modules["title"] = projection.ModuleView{Name: "title", IsEnabled: true, Configs: codec.RawMessage(tc.config)}
			}
			p := newPipelineWith(&fakePublisher{}, fakeReader{modules: modules}, b.Build())
			_, _ = runChat(t, p, chatEnv("!settitle New title", tc.badge))
			assert.Equal(t, tc.wantRuns, runs)
		})
	}
}

func TestBakedCommandShadowsCustomWhenItsModuleAllows(t *testing.T) {
	enabled := []projection.ModuleView{{Name: "urchin", IsEnabled: true}}
	cases := []struct {
		name    string
		beta    bool
		modules []projection.ModuleView
		lane    string
		want    string
	}{
		{"disabled falls through to custom", false, nil, "standard", "custom daily"},
		{"enabled wins over custom", false, enabled, "standard", "baked daily"},
		{"beta standard lane falls through", true, enabled, "standard", "custom daily"},
		{"beta premium lane runs", true, enabled, "premium", "baked daily"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reader := fakeReader{
				cmd:      projection.Command{Name: "daily", Response: "custom daily", IsActive: true, Perm: "everyone"},
				cmdFound: true,
				modules:  projection.ModuleMap(tc.modules),
			}
			mod := cmdEmit("urchin", module.KindOptIn, "daily", "baked daily")
			mod.Beta = tc.beta
			pub := &fakePublisher{}

			require.NoError(t, newPipelineWith(pub, reader, mod).Process(chatMsg(t, tc.lane, "!daily")))

			assert.Equal(t, []string{tc.want}, pub.chatTexts(t))
		})
	}
}

func TestBakedCommandFollowsItsModuleGate(t *testing.T) {
	cases := []struct {
		name    string
		kind    module.Kind
		modules []projection.ModuleView
		want    []string
	}{
		{"an opt-in command stays silent without its module row", module.KindOptIn, nil, nil},
		{"an opt-in command runs once its module is enabled", module.KindOptIn, []projection.ModuleView{{Name: "extra", IsEnabled: true}}, []string{"yo"}},
		{"a default command runs without a module row", module.KindDefault, nil, []string{"yo"}},
		{"a default command stays silent once its module is disabled", module.KindDefault, []projection.ModuleView{{Name: "extra", IsEnabled: false}}, nil},
		{"a core command runs regardless of its module", module.KindCore, nil, []string{"yo"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pub := &fakePublisher{}
			reader := fakeReader{modules: projection.ModuleMap(tc.modules)}
			p := newPipelineWith(pub, reader, cmdEmit("extra", tc.kind, "hi", "yo"))

			require.NoError(t, p.Process(chatMsg(t, "standard", "!hi")))

			assert.Equal(t, tc.want, pub.chatTexts(t))
		})
	}
}

func TestCommandBumpCounterOption(t *testing.T) {
	cases := []struct {
		name      string
		command   projection.Command
		wantReply int
		wantBumps []CounterBump
	}{
		{
			name:      "bumps once on a successful run",
			command:   projection.Command{Name: "so", Response: "hi", IsActive: true, Perm: "everyone", BumpCounter: "deaths"},
			wantReply: 1,
			wantBumps: []CounterBump{{BroadcasterID: 123, Name: "deaths", Viewer: Viewer{ID: 999, Login: "alice"}, Command: "so", Delta: 1}},
		},
		{
			name:      "never bumps without the option",
			command:   projection.Command{Name: "so", Response: "hi", IsActive: true, Perm: "everyone"},
			wantReply: 1,
		},
		{
			name:    "never bumps when the gate refuses the sender",
			command: projection.Command{Name: "so", Response: "hi", IsActive: true, AllowedUserID: "555", BumpCounter: "deaths"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			loyalty := &stubLoyalty{}
			got := replies(t, customCommandPipeline(tc.command, loyalty), chatEnv("!so", ""))

			assert.Len(t, got, tc.wantReply)
			assert.Equal(t, tc.wantBumps, loyalty.bumps)
		})
	}
}

func TestCommandBumpCounterOptionRedeliveryDoesNotDoubleCount(t *testing.T) {
	store := newRecordingStore()
	loyalty := &stubLoyalty{}
	d := Deps{
		Proj: fakeReader{
			cmd:      projection.Command{Name: "so", Response: "hi", IsActive: true, BumpCounter: "deaths"},
			cmdFound: true,
		},
		Live: liveAlways{}, Cooldown: NoopCooldown{},
		Pub: &fakePublisher{}, Log: zap.NewNop(),
		Loyalty: loyalty,
		Dedup:   NewEventDedup(store, "sesame:seen:", time.Minute, zap.NewNop()),
	}
	p := NewPipeline(d, NewRegistry(zap.NewNop()), Config{OutgressPremium: premiumSubj, OutgressStandard: standardSubj})

	require.NoError(t, p.Process(commandMsg(t, "m1", "!so")))
	require.NoError(t, p.Process(commandMsg(t, "m1", "!so")))

	require.Len(t, loyalty.bumps, 1, "a replayed command must bump once, not twice")
	assert.Contains(t, store.keys(), "m1:"+CounterEffect("deaths"))
}

type fakeReader struct {
	user     projection.User
	modules  map[string]projection.ModuleView
	modErr   error
	cmd      projection.Command
	cmdFound bool
}

func (r fakeReader) User(context.Context, uint64) (projection.User, error) { return r.user, nil }

func (r fakeReader) Modules(context.Context, uint64) (map[string]projection.ModuleView, error) {
	return r.modules, r.modErr
}

func (r fakeReader) Module(ctx context.Context, id uint64, name string) (projection.ModuleView, bool, error) {
	views, err := r.Modules(ctx, id)
	if err != nil {
		return projection.ModuleView{}, false, err
	}
	view, ok := views[name]
	return view, ok, nil
}

func (r fakeReader) Command(context.Context, uint64, string) (projection.Command, bool, error) {
	return r.cmd, r.cmdFound, nil
}

type liveAlways struct{}

func (liveAlways) IsLive(context.Context, uint64) (bool, error) { return true, nil }

func (liveAlways) SetLive(context.Context, uint64, int64) (bool, error) { return true, nil }

func (liveAlways) ClearLive(context.Context, uint64, int64) (bool, error) { return true, nil }

func emitModule(name string, kind module.Kind, text string) module.Module {
	b := module.NewModule(name, kind)
	b.On(chatType, func(_ context.Context, c *module.Context, emit module.Emit) error {
		o := GetOutput()
		defer PutOutput(o)
		o.Type = outgress.TypeChat
		o.BroadcasterID = c.Env.BroadcasterUserID
		o.Text = text
		emit(o)
		return nil
	})
	return b.Build()
}

func cmdEmit(name string, kind module.Kind, trigger, reply string) module.Module {
	b := module.NewModule(name, kind)
	b.Command(trigger).Everyone().Run(func(_ context.Context, c *module.Context, _ string, emit module.Emit) error {
		emit(&module.Output{Type: outgress.TypeChat, BroadcasterID: c.Env.BroadcasterUserID, Text: reply})
		return nil
	})
	return b.Build()
}
