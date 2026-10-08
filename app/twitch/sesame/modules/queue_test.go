// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"context"
	"testing"

	"ItsBagelBot/app/twitch/sesame/engine"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type fakeQueue struct {
	open bool
	line []string
	err  error
}

func (f *fakeQueue) SetOpen(_ context.Context, _ uint64, open bool) error {
	if f.err != nil {
		return f.err
	}
	f.open = open
	return nil
}

func (f *fakeQueue) IsOpen(_ context.Context, _ uint64) (bool, error) {
	return f.open, f.err
}

func (f *fakeQueue) indexOf(login string) int {
	for i, l := range f.line {
		if l == login {
			return i
		}
	}
	return -1
}

func (f *fakeQueue) Join(_ context.Context, _ uint64, login string) (pos, size int64, joined bool, err error) {
	if f.err != nil {
		return 0, 0, false, f.err
	}
	if i := f.indexOf(login); i >= 0 {
		return int64(i + 1), int64(len(f.line)), false, nil
	}
	f.line = append(f.line, login)
	return int64(len(f.line)), int64(len(f.line)), true, nil
}

func (f *fakeQueue) Remove(_ context.Context, _ uint64, login string) (bool, error) {
	if f.err != nil {
		return false, f.err
	}
	i := f.indexOf(login)
	if i < 0 {
		return false, nil
	}
	f.line = append(f.line[:i], f.line[i+1:]...)
	return true, nil
}

func (f *fakeQueue) Pop(_ context.Context, _ uint64) (string, int64, error) {
	if f.err != nil {
		return "", 0, f.err
	}
	if len(f.line) == 0 {
		return "", 0, nil
	}
	head := f.line[0]
	f.line = f.line[1:]
	return head, int64(len(f.line)), nil
}

func (f *fakeQueue) List(_ context.Context, _ uint64, n int64) ([]string, int64, error) {
	if f.err != nil {
		return nil, 0, f.err
	}
	total := int64(len(f.line))
	if n > total {
		n = total
	}
	out := make([]string, n)
	copy(out, f.line[:n])
	return out, total, nil
}

func (f *fakeQueue) Clear(_ context.Context, _ uint64) error {
	if f.err != nil {
		return f.err
	}
	f.line = nil
	return nil
}

func queueDeps(q engine.QueueStore) engine.Deps {
	return engine.Deps{Queue: q, Log: zap.NewNop()}
}

func TestQueueChat(t *testing.T) {
	thirteen := make([]string, 13)
	for i := range thirteen {
		thirteen[i] = string(rune('a' + i))
	}
	cases := []struct {
		name     string
		text     string
		who      string
		badge    string
		config   string
		queue    fakeQueue
		silent   bool
		exact    string
		contains []string
		excludes []string
		line     []string
	}{
		{name: "joining an open queue takes the next spot", text: "!join", who: "alice", queue: fakeQueue{open: true}, contains: []string{"#1"}, line: []string{"alice"}},
		{name: "joining a closed queue is refused", text: "!join", who: "alice", contains: []string{"closed"}},
		{name: "joining twice keeps the spot", text: "!join", who: "alice", queue: fakeQueue{open: true, line: []string{"bob", "alice"}},
			contains: []string{"already", "#2"}, line: []string{"bob", "alice"}},
		{name: "the queue subcommand joins too", text: "!queue join", who: "alice", queue: fakeQueue{open: true}, contains: []string{"#1"}, line: []string{"alice"}},
		{name: "leaving frees the spot", text: "!leave", who: "alice", queue: fakeQueue{open: true, line: []string{"alice", "bob"}}, contains: []string{"alice"}, line: []string{"bob"}},
		{name: "leaving without a spot says so", text: "!leave", who: "alice", queue: fakeQueue{open: true, line: []string{"bob"}}, contains: []string{"not in the queue"}, line: []string{"bob"}},
		{name: "an empty list says so", text: "!list", who: "alice", queue: fakeQueue{open: true}, contains: []string{"empty"}},
		{name: "the list numbers the line", text: "!list", who: "alice", queue: fakeQueue{open: true, line: []string{"a", "b", "c"}},
			contains: []string{"1. a", "2. b", "3. c"}, line: []string{"a", "b", "c"}},
		{name: "the queue subcommand lists too", text: "!queue list", who: "bob", queue: fakeQueue{open: true, line: []string{"alice"}}, contains: []string{"1. alice"}, line: []string{"alice"}},
		{name: "the list truncates to ten with a remainder", text: "!list", who: "alice", queue: fakeQueue{open: true, line: thirteen},
			contains: []string{"10. j", "+3 more"}, excludes: []string{"11. k"}, line: thirteen},
		{name: "the list ignores the join template", text: "!list", who: "alice", config: `{"joinMessage":"custom"}`, queue: fakeQueue{open: true, line: []string{"a", "b"}},
			contains: []string{"1. a", "2. b"}, excludes: []string{"custom"}, line: []string{"a", "b"}},
		{name: "a mod calls the next viewer", text: "!queue next", who: "mod", badge: "moderator", queue: fakeQueue{open: true, line: []string{"alice", "bob"}},
			contains: []string{"@alice", "1 still waiting"}, line: []string{"bob"}},
		{name: "next on an empty queue says so", text: "!queue next", who: "mod", badge: "moderator", queue: fakeQueue{open: true}, contains: []string{"empty"}},
		{name: "a mod removes a viewer", text: "!queue remove @alice", who: "mod", badge: "moderator", queue: fakeQueue{open: true, line: []string{"alice", "bob"}},
			contains: []string{"@alice"}, line: []string{"bob"}},
		{name: "removing an absent viewer says so", text: "!queue remove alice", who: "mod", badge: "moderator", queue: fakeQueue{open: true, line: []string{"bob"}},
			contains: []string{"not in the queue"}, line: []string{"bob"}},
		{name: "a viewer's remove is ignored", text: "!queue remove bob", who: "alice", queue: fakeQueue{open: true, line: []string{"alice", "bob"}},
			silent: true, line: []string{"alice", "bob"}},
		{name: "a mod clears the line", text: "!queue clear", who: "mod", badge: "moderator", queue: fakeQueue{open: true, line: []string{"a", "b"}}, contains: []string{"clear"}},
		{name: "the bare command reports the status", text: "!queue", who: "alice", queue: fakeQueue{open: true, line: []string{"a", "b"}},
			contains: []string{"open", "2"}, line: []string{"a", "b"}},
		{name: "the join template fills the spot", text: "!join", who: "alice", config: `{"joinMessage":"welcome {user}! spot {pos}"}`, queue: fakeQueue{open: true},
			exact: "welcome alice! spot 1", line: []string{"alice"}},
		{name: "the next template fills the target and count", text: "!queue next", who: "mod", badge: "moderator", config: `{"nextMessage":"{target} is up, {count} left"}`,
			queue: fakeQueue{open: true, line: []string{"alice", "bob"}}, exact: "alice is up, 1 left", line: []string{"bob"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			q := tc.queue
			out := runChat(t, Queue(queueDeps(&q)), withConfig(chatCtx("42", tc.who, tc.badge), tc.config), tc.text)
			if tc.silent {
				assert.Empty(t, out)
			} else {
				require.Len(t, out, 1)
				assertText(t, out[0].Text, textWant{tc.exact, tc.contains, tc.excludes})
			}
			assert.Equal(t, tc.line, q.line)
		})
	}
}

func TestQueueOpenAndClose(t *testing.T) {
	q := &fakeQueue{}
	m := Queue(queueDeps(q))

	assert.Empty(t, runChat(t, m, chatCtx("42", "alice"), "!queue open"))
	assert.False(t, q.open, "a viewer cannot open the queue")

	out := runChat(t, m, chatCtx("9", "mod", "moderator"), "!queue open")
	require.Len(t, out, 1)
	assert.Contains(t, out[0].Text, "open")
	assert.True(t, q.open)

	require.Len(t, runChat(t, m, chatCtx("100", "streamer"), "!queue close"), 1)
	assert.False(t, q.open, "the broadcaster closes it")
}

func TestQueueStaysSilentWithoutAStore(t *testing.T) {
	assert.Empty(t, runChat(t, Queue(queueDeps(nil)), chatCtx("42", "alice"), "!join"))
}

func TestQueueListSubcommandSharesCooldown(t *testing.T) {
	cd := &fakeCooldown{allow: []bool{true, false}}
	d := queueDeps(&fakeQueue{open: true, line: []string{"alice"}})
	d.Cooldown = cd
	m := Queue(d)
	require.Len(t, runChat(t, m, chatCtx("42", "alice"), "!queue list"), 1)
	require.Equal(t, []string{engine.CommandCooldownKey(100, "list")}, cd.keys)
	assert.Empty(t, runChat(t, m, chatCtx("43", "bob"), "!queue list"))
}
