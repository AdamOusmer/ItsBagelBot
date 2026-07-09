package modules

import (
	"context"
	"testing"

	"ItsBagelBot/app/sesame/engine"
	"ItsBagelBot/app/sesame/module"
	"ItsBagelBot/internal/domain/event/lane"
	"ItsBagelBot/internal/domain/outgress"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// fakeQueue is an in-memory QueueStore for the module tests: a slice preserves
// join order, a bool the open flag.
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

// queueCtx builds a Context for chatter `login`. badge, when non-empty, is a
// Twitch badge set_id ("moderator", "broadcaster", …) so the role gate resolves.
func queueCtx(login, badge string) *module.Context {
	env := lane.Envelope{
		Type:                 "channel.chat.message",
		BroadcasterUserID:    "100",
		BroadcasterUserLogin: "streamer",
		ChatterUserID:        "42",
		ChatterUserLogin:     login,
	}
	if badge != "" {
		env.Badges = []lane.Badge{{SetID: badge}}
	}
	return &module.Context{Env: env, BroadcasterID: 100, Log: zap.NewNop()}
}

func runQueue(t *testing.T, m module.Module, name string, c *module.Context, args string) []module.Output {
	t.Helper()
	cmd := findCmd(t, m, name)
	var col collector
	require.NoError(t, cmd.Run(context.Background(), c, args, col.emit))
	return col.out
}

// --- join ---

func TestQueueJoinOpen(t *testing.T) {
	q := &fakeQueue{open: true}
	m := Queue(queueDeps(q))

	out := runQueue(t, m, "join", queueCtx("alice", ""), "")
	require.Len(t, out, 1)
	assert.Equal(t, outgress.TypeChat, out[0].Type)
	assert.Contains(t, out[0].Text, "#1")
	assert.Equal(t, []string{"alice"}, q.line)
}

func TestQueueJoinClosed(t *testing.T) {
	q := &fakeQueue{open: false}
	m := Queue(queueDeps(q))

	out := runQueue(t, m, "join", queueCtx("alice", ""), "")
	require.Len(t, out, 1)
	assert.Contains(t, out[0].Text, "closed")
	assert.Empty(t, q.line)
}

func TestQueueJoinTwiceKeepsSpot(t *testing.T) {
	q := &fakeQueue{open: true, line: []string{"bob", "alice"}}
	m := Queue(queueDeps(q))

	out := runQueue(t, m, "join", queueCtx("alice", ""), "")
	require.Len(t, out, 1)
	assert.Contains(t, out[0].Text, "already")
	assert.Contains(t, out[0].Text, "#2")
	assert.Equal(t, []string{"bob", "alice"}, q.line)
}

// --- leave ---

func TestQueueLeave(t *testing.T) {
	q := &fakeQueue{open: true, line: []string{"alice", "bob"}}
	m := Queue(queueDeps(q))

	out := runQueue(t, m, "leave", queueCtx("alice", ""), "")
	require.Len(t, out, 1)
	assert.Equal(t, []string{"bob"}, q.line)
}

func TestQueueLeaveNotIn(t *testing.T) {
	q := &fakeQueue{open: true, line: []string{"bob"}}
	m := Queue(queueDeps(q))

	out := runQueue(t, m, "leave", queueCtx("alice", ""), "")
	require.Len(t, out, 1)
	assert.Contains(t, out[0].Text, "not in the queue")
}

// --- list ---

func TestQueueListEmpty(t *testing.T) {
	q := &fakeQueue{open: true}
	m := Queue(queueDeps(q))

	out := runQueue(t, m, "list", queueCtx("alice", ""), "")
	require.Len(t, out, 1)
	assert.Contains(t, out[0].Text, "empty")
}

func TestQueueListNumbers(t *testing.T) {
	q := &fakeQueue{open: true, line: []string{"a", "b", "c"}}
	m := Queue(queueDeps(q))

	out := runQueue(t, m, "list", queueCtx("alice", ""), "")
	require.Len(t, out, 1)
	assert.Contains(t, out[0].Text, "1. a")
	assert.Contains(t, out[0].Text, "2. b")
	assert.Contains(t, out[0].Text, "3. c")
}

func TestQueueListTruncatesToTen(t *testing.T) {
	line := make([]string, 13)
	for i := range line {
		line[i] = string(rune('a' + i))
	}
	q := &fakeQueue{open: true, line: line}
	m := Queue(queueDeps(q))

	out := runQueue(t, m, "list", queueCtx("alice", ""), "")
	require.Len(t, out, 1)
	assert.Contains(t, out[0].Text, "10. j")
	assert.NotContains(t, out[0].Text, "11. k")
	assert.Contains(t, out[0].Text, "+3 more")
}

// --- !queue subcommands: mod gate ---

func TestQueueOpenRequiresMod(t *testing.T) {
	q := &fakeQueue{}
	m := Queue(queueDeps(q))

	// non-mod: silent no-op, flag unchanged.
	out := runQueue(t, m, "queue", queueCtx("alice", ""), "open")
	assert.Empty(t, out)
	assert.False(t, q.open)

	// mod: opens and confirms.
	out = runQueue(t, m, "queue", queueCtx("mod", "moderator"), "open")
	require.Len(t, out, 1)
	assert.Contains(t, out[0].Text, "open")
	assert.True(t, q.open)
}

func TestQueueCloseByBroadcaster(t *testing.T) {
	q := &fakeQueue{open: true}
	m := Queue(queueDeps(q))

	// broadcaster resolves via ChatterUserID == BroadcasterUserID.
	c := queueCtx("streamer", "")
	c.Env.ChatterUserID = "100"
	out := runQueue(t, m, "queue", c, "close")
	require.Len(t, out, 1)
	assert.False(t, q.open)
}

func TestQueueNext(t *testing.T) {
	q := &fakeQueue{open: true, line: []string{"alice", "bob"}}
	m := Queue(queueDeps(q))

	out := runQueue(t, m, "queue", queueCtx("mod", "moderator"), "next")
	require.Len(t, out, 1)
	assert.Contains(t, out[0].Text, "@alice")
	assert.Contains(t, out[0].Text, "1 still waiting")
	assert.Equal(t, []string{"bob"}, q.line)
}

func TestQueueNextEmpty(t *testing.T) {
	q := &fakeQueue{open: true}
	m := Queue(queueDeps(q))

	out := runQueue(t, m, "queue", queueCtx("mod", "moderator"), "next")
	require.Len(t, out, 1)
	assert.Contains(t, out[0].Text, "empty")
}

func TestQueueRemoveByMod(t *testing.T) {
	q := &fakeQueue{open: true, line: []string{"alice", "bob"}}
	m := Queue(queueDeps(q))

	out := runQueue(t, m, "queue", queueCtx("mod", "moderator"), "remove @alice")
	require.Len(t, out, 1)
	assert.Contains(t, out[0].Text, "@alice")
	assert.Equal(t, []string{"bob"}, q.line)
}

func TestQueueRemoveNotFound(t *testing.T) {
	q := &fakeQueue{open: true, line: []string{"bob"}}
	m := Queue(queueDeps(q))

	out := runQueue(t, m, "queue", queueCtx("mod", "moderator"), "remove alice")
	require.Len(t, out, 1)
	assert.Contains(t, out[0].Text, "not in the queue")
	assert.Equal(t, []string{"bob"}, q.line)
}

func TestQueueRemoveNonModIgnored(t *testing.T) {
	q := &fakeQueue{open: true, line: []string{"alice", "bob"}}
	m := Queue(queueDeps(q))

	out := runQueue(t, m, "queue", queueCtx("alice", ""), "remove bob")
	assert.Empty(t, out)
	assert.Equal(t, []string{"alice", "bob"}, q.line)
}

func TestQueueClear(t *testing.T) {
	q := &fakeQueue{open: true, line: []string{"a", "b"}}
	m := Queue(queueDeps(q))

	out := runQueue(t, m, "queue", queueCtx("mod", "moderator"), "clear")
	require.Len(t, out, 1)
	assert.Empty(t, q.line)
}

// --- status + nil store ---

func TestQueueStatus(t *testing.T) {
	q := &fakeQueue{open: true, line: []string{"a", "b"}}
	m := Queue(queueDeps(q))

	out := runQueue(t, m, "queue", queueCtx("alice", ""), "")
	require.Len(t, out, 1)
	assert.Contains(t, out[0].Text, "open")
	assert.Contains(t, out[0].Text, "2")
}

func TestQueueNilStoreInert(t *testing.T) {
	m := Queue(queueDeps(nil))
	out := runQueue(t, m, "join", queueCtx("alice", ""), "")
	assert.Empty(t, out)
}

func TestQueueJoinAndListViaSubcommand(t *testing.T) {
	q := &fakeQueue{open: true}
	m := Queue(queueDeps(q))

	runQueue(t, m, "queue", queueCtx("alice", ""), "join")
	assert.Equal(t, []string{"alice"}, q.line)

	out := runQueue(t, m, "queue", queueCtx("bob", ""), "list")
	require.Len(t, out, 1)
	assert.Contains(t, out[0].Text, "1. alice")
}

// --- customizable reply templates ---

func TestQueueJoinCustomTemplate(t *testing.T) {
	q := &fakeQueue{open: true}
	m := Queue(queueDeps(q))

	c := queueCtx("alice", "")
	c.Config = []byte(`{"joinMessage":"welcome {user}! spot {pos}"}`)
	out := runQueue(t, m, "join", c, "")
	require.Len(t, out, 1)
	assert.Equal(t, "welcome alice! spot 1", out[0].Text)
}

func TestQueueNextCustomTemplate(t *testing.T) {
	q := &fakeQueue{open: true, line: []string{"alice", "bob"}}
	m := Queue(queueDeps(q))

	c := queueCtx("mod", "moderator")
	c.Config = []byte(`{"nextMessage":"{target} is up, {count} left"}`)
	out := runQueue(t, m, "queue", c, "next")
	require.Len(t, out, 1)
	assert.Equal(t, "alice is up, 1 left", out[0].Text)
}

// A blank custom template falls back to the localized default; the roster is
// never customizable, so a stray config key cannot alter it.
func TestQueueListIgnoresConfig(t *testing.T) {
	q := &fakeQueue{open: true, line: []string{"a", "b"}}
	m := Queue(queueDeps(q))

	c := queueCtx("alice", "")
	c.Config = []byte(`{"joinMessage":"custom"}`)
	out := runQueue(t, m, "list", c, "")
	require.Len(t, out, 1)
	assert.Contains(t, out[0].Text, "1. a")
	assert.Contains(t, out[0].Text, "2. b")
}
