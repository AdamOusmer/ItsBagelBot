// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"testing"

	"ItsBagelBot/app/twitch/sesame/engine"
	"ItsBagelBot/app/twitch/sesame/engine/draw"
	"ItsBagelBot/app/twitch/sesame/engine/eligibility"
	"ItsBagelBot/internal/projection"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const raffleJoinOn = `{"joinEnabled":"on"}`

type raffleJoinCase struct {
	name    string
	config  string
	open    bool
	repeat  bool
	queue   *fakeQueue
	queueOn bool
	want    textWant
}

func TestRaffleJoin(t *testing.T) {
	openQueue := &fakeQueue{open: true}
	for _, tc := range []raffleJoinCase{
		{name: "joining an open raffle is silent by default", open: true},
		{name: "joining an open raffle counts the entry", config: raffleJoinOn, open: true,
			want: textWant{contains: []string{"@alice", "1 entered"}, excludes: []string{"!queue join"}}},
		{name: "joining a closed raffle is refused", want: textWant{contains: []string{"no raffle"}}},
		{name: "the no raffle line can be turned off", config: `{"noRaffleEnabled":"off"}`},
		{name: "the no raffle line can be explicitly enabled", config: `{"noRaffleEnabled":"on"}`,
			want: textWant{contains: []string{"no raffle"}}},
		{name: "joining twice is silent by default", open: true, repeat: true},
		{name: "joining twice keeps one entry", config: `{"alreadyEnabled":"true"}`, open: true, repeat: true,
			want: textWant{contains: []string{"already", "1 entered"}}},
		{name: "the already line stays off when only join is on", config: raffleJoinOn, open: true, repeat: true},
		{name: "the join template fills the count", config: `{"joinEnabled":"on","joinMessage":"welcome {user}! {count} in so far"}`, open: true,
			want: textWant{exact: "welcome alice! 1 in so far"}},
		{name: "a join template stays silent while the line is off", config: `{"joinMessage":"welcome"}`, open: true},
		{name: "an open queue is pointed to", config: raffleJoinOn, open: true, queue: openQueue, queueOn: true,
			want: textWant{contains: []string{"1 entered", "!queue join"}}},
		{name: "a repeat entry also points to the queue", config: `{"alreadyEnabled":"on"}`, open: true, repeat: true, queue: openQueue, queueOn: true,
			want: textWant{contains: []string{"already", "!queue join"}}},
		{name: "a closed queue is not pointed to", config: raffleJoinOn, open: true, queue: &fakeQueue{}, queueOn: true,
			want: textWant{contains: []string{"1 entered"}, excludes: []string{"!queue join"}}},
		{name: "a disabled queue module is not pointed to", config: raffleJoinOn, open: true, queue: openQueue,
			want: textWant{contains: []string{"1 entered"}, excludes: []string{"!queue join"}}},
		{name: "a custom join template gets no queue hint", config: `{"joinEnabled":"on","joinMessage":"in!"}`, open: true, queue: openQueue, queueOn: true,
			want: textWant{exact: "in!"}},
	} {
		t.Run(tc.name, tc.run)
	}
}

func (tc raffleJoinCase) run(t *testing.T) {
	checker := eligibility.NewChecker(eligibility.Ports{})
	t.Cleanup(checker.Close)
	store := &fakeDraw{joinable: tc.open, kind: draw.KindRaffle}
	proj := &fakeProj{modules: []projection.ModuleView{enabledVariables(raffleModuleName, tc.config)}}
	deps := engine.Deps{Draw: store, Eligibility: checker, Proj: proj}
	if tc.queue != nil {
		deps.Queue = tc.queue
	}
	m := Draw(deps)
	c := chatCtx("42", "alice")
	c.ModuleLookup = func(name string) bool { return name == raffleModuleName || tc.queueModuleOn(name) }
	if tc.repeat {
		runChat(t, m, c, "!join")
	}
	out := runChat(t, m, c, "!join")
	assert.Equal(t, tc.open, store.joined.Viewer == "42")
	if tc.want.exact == "" && len(tc.want.contains) == 0 {
		assert.Empty(t, out)
		return
	}
	require.Len(t, out, 1)
	assertText(t, out[0].Text, tc.want)
}

func (tc raffleJoinCase) queueModuleOn(name string) bool {
	return tc.queueOn && name == queueModuleName
}
