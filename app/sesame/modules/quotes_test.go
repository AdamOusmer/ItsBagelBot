package modules

import (
	"context"
	"testing"
	"time"

	"ItsBagelBot/app/sesame/engine"
	"ItsBagelBot/app/sesame/module"
	"ItsBagelBot/internal/domain/event/lane"
	"ItsBagelBot/internal/domain/outgress"
	modulesrpc "ItsBagelBot/internal/domain/rpc/modules"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// fakeQuotes is an in-memory QuotesStore keyed by number. Adds number max+1;
// random returns the lowest number for determinism.
type fakeQuotes struct {
	quotes map[uint64]modulesrpc.Quote
	err    error
}

func newFakeQuotes(texts ...string) *fakeQuotes {
	f := &fakeQuotes{quotes: map[uint64]modulesrpc.Quote{}}
	for i, text := range texts {
		n := uint64(i + 1)
		f.quotes[n] = modulesrpc.Quote{Number: n, Text: text, CreatedAt: "2026-07-10T15:04:05Z"}
	}
	return f
}

func (f *fakeQuotes) QuoteAdd(_ context.Context, _ uint64, text, addedBy string) (modulesrpc.Quote, error) {
	if f.err != nil {
		return modulesrpc.Quote{}, f.err
	}
	var max uint64
	for n := range f.quotes {
		if n > max {
			max = n
		}
	}
	q := modulesrpc.Quote{Number: max + 1, Text: text, AddedBy: addedBy, CreatedAt: "2026-07-11T10:00:00Z"}
	f.quotes[q.Number] = q
	return q, nil
}

func (f *fakeQuotes) QuoteGet(_ context.Context, _ uint64, number uint64) (modulesrpc.Quote, bool, error) {
	if f.err != nil {
		return modulesrpc.Quote{}, false, f.err
	}
	q, ok := f.quotes[number]
	return q, ok, nil
}

func (f *fakeQuotes) QuoteRandom(_ context.Context, _ uint64) (modulesrpc.Quote, bool, error) {
	if f.err != nil {
		return modulesrpc.Quote{}, false, f.err
	}
	var best modulesrpc.Quote
	found := false
	for _, q := range f.quotes {
		if !found || q.Number < best.Number {
			best, found = q, true
		}
	}
	return best, found, nil
}

func (f *fakeQuotes) QuoteRemove(_ context.Context, _ uint64, number uint64) (bool, error) {
	if f.err != nil {
		return false, f.err
	}
	_, ok := f.quotes[number]
	delete(f.quotes, number)
	return ok, nil
}

// countingCooldown records claims and answers a fixed verdict.
type countingCooldown struct {
	allow  bool
	claims int
}

func (c *countingCooldown) Allow(context.Context, string, time.Duration) (bool, error) {
	c.claims++
	return c.allow, nil
}

func quotesCtx(login, badge string) *module.Context {
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

func runQuotes(t *testing.T, d engine.Deps, c *module.Context, args string) []module.Output {
	t.Helper()
	m := Quotes(d)
	cmd := findCmd(t, m, "quote")
	var col collector
	require.NoError(t, cmd.Run(context.Background(), c, args, col.emit))
	return col.out
}

// withAddPerm sets the module's addPerm config on a context, as the engine
// would from the broadcaster's ModuleView.
func withAddPerm(c *module.Context, perm string) *module.Context {
	c.Config = []byte(`{"addPerm":"` + perm + `"}`)
	return c
}

func TestQuoteRandomReadout(t *testing.T) {
	f := newFakeQuotes("never trust a ferret")
	out := runQuotes(t, engine.Deps{Quotes: f}, quotesCtx("alice", ""), "")

	require.Len(t, out, 1)
	assert.Equal(t, outgress.TypeChat, out[0].Type)
	assert.Equal(t, "100", out[0].BroadcasterID)
	assert.Equal(t, `Quote #1: never trust a ferret (2026-07-10)`, out[0].Text)
}

func TestQuoteByNumber(t *testing.T) {
	f := newFakeQuotes("one", "two")
	out := runQuotes(t, engine.Deps{Quotes: f}, quotesCtx("alice", ""), "2")

	require.Len(t, out, 1)
	assert.Equal(t, `Quote #2: two (2026-07-10)`, out[0].Text)
}

func TestQuoteByNumberMissing(t *testing.T) {
	f := newFakeQuotes("one")
	out := runQuotes(t, engine.Deps{Quotes: f}, quotesCtx("alice", ""), "7")

	require.Len(t, out, 1)
	assert.Contains(t, out[0].Text, "#7")
	assert.Contains(t, out[0].Text, "doesn't exist")
}

func TestQuoteEmptyBook(t *testing.T) {
	f := newFakeQuotes()
	out := runQuotes(t, engine.Deps{Quotes: f}, quotesCtx("alice", ""), "")

	require.Len(t, out, 1)
	assert.Contains(t, out[0].Text, "No quotes saved yet")
}

func TestQuoteAddQuotedByMod(t *testing.T) {
	f := newFakeQuotes("existing")
	out := runQuotes(t, engine.Deps{Quotes: f}, quotesCtx("mod_amy", "moderator"), `"the bagels are sentient"`)

	require.Len(t, out, 1)
	assert.Contains(t, out[0].Text, "#2")
	assert.Contains(t, out[0].Text, "added")
	assert.Equal(t, "the bagels are sentient", f.quotes[2].Text)
	assert.Equal(t, "mod_amy", f.quotes[2].AddedBy)
}

func TestQuoteAddCurlyQuotes(t *testing.T) {
	f := newFakeQuotes()
	out := runQuotes(t, engine.Deps{Quotes: f}, quotesCtx("mod_amy", "moderator"), "“smart quotes too”")

	require.Len(t, out, 1)
	assert.Equal(t, "smart quotes too", f.quotes[1].Text)
}

func TestQuoteAddSubcommand(t *testing.T) {
	f := newFakeQuotes()
	out := runQuotes(t, engine.Deps{Quotes: f}, quotesCtx("streamer", "broadcaster"), "add plain words work")

	require.Len(t, out, 1)
	assert.Equal(t, "plain words work", f.quotes[1].Text)
}

func TestQuoteAddByViewerIsSilent(t *testing.T) {
	f := newFakeQuotes()
	out := runQuotes(t, engine.Deps{Quotes: f}, quotesCtx("alice", ""), `"nice try"`)

	assert.Empty(t, out)
	assert.Empty(t, f.quotes)
}

func TestQuoteAddPermSubscriberAllowsSub(t *testing.T) {
	f := newFakeQuotes()
	ctx := withAddPerm(quotesCtx("subby", "subscriber"), "sub")
	out := runQuotes(t, engine.Deps{Quotes: f}, ctx, `"subs can save now"`)

	require.Len(t, out, 1)
	assert.Contains(t, out[0].Text, "added")
	assert.Equal(t, "subs can save now", f.quotes[1].Text)
}

func TestQuoteAddPermSubscriberBlocksViewer(t *testing.T) {
	f := newFakeQuotes()
	ctx := withAddPerm(quotesCtx("alice", ""), "sub")
	out := runQuotes(t, engine.Deps{Quotes: f}, ctx, `"no badge here"`)

	assert.Empty(t, out)
	assert.Empty(t, f.quotes)
}

func TestQuoteAddPermEveryoneAllowsViewer(t *testing.T) {
	f := newFakeQuotes()
	ctx := withAddPerm(quotesCtx("alice", ""), "everyone")
	out := runQuotes(t, engine.Deps{Quotes: f}, ctx, `"anyone can save"`)

	require.Len(t, out, 1)
	assert.Contains(t, out[0].Text, "added")
	assert.Equal(t, "anyone can save", f.quotes[1].Text)
}

// Remove stays moderator-only even when saving is opened to everyone.
func TestQuoteRemoveIgnoresAddPerm(t *testing.T) {
	f := newFakeQuotes("one")
	ctx := withAddPerm(quotesCtx("alice", ""), "everyone")
	out := runQuotes(t, engine.Deps{Quotes: f}, ctx, "remove 1")

	assert.Empty(t, out)
	_, ok := f.quotes[1]
	assert.True(t, ok)
}

func TestQuoteRemoveByMod(t *testing.T) {
	f := newFakeQuotes("one", "two")
	out := runQuotes(t, engine.Deps{Quotes: f}, quotesCtx("mod_amy", "moderator"), "remove 1")

	require.Len(t, out, 1)
	assert.Contains(t, out[0].Text, "#1")
	assert.Contains(t, out[0].Text, "removed")
	_, ok := f.quotes[1]
	assert.False(t, ok)
}

func TestQuoteRemoveByViewerIsSilent(t *testing.T) {
	f := newFakeQuotes("one")
	out := runQuotes(t, engine.Deps{Quotes: f}, quotesCtx("alice", ""), "remove 1")

	assert.Empty(t, out)
	_, ok := f.quotes[1]
	assert.True(t, ok)
}

func TestQuoteRemoveUsage(t *testing.T) {
	f := newFakeQuotes("one")
	out := runQuotes(t, engine.Deps{Quotes: f}, quotesCtx("mod_amy", "moderator"), "remove ferret")

	require.Len(t, out, 1)
	assert.Contains(t, out[0].Text, "Usage")
}

func TestQuoteUnknownArgsUsage(t *testing.T) {
	f := newFakeQuotes("one")
	out := runQuotes(t, engine.Deps{Quotes: f}, quotesCtx("alice", ""), "something funny")

	require.Len(t, out, 1)
	assert.Contains(t, out[0].Text, "Usage")
}

func TestQuoteReadCooldownThrottles(t *testing.T) {
	f := newFakeQuotes("one")
	cd := &countingCooldown{allow: false}
	out := runQuotes(t, engine.Deps{Quotes: f, Cooldown: cd}, quotesCtx("alice", ""), "")

	assert.Empty(t, out)
	assert.Equal(t, 1, cd.claims)
}

func TestQuoteAddSkipsCooldown(t *testing.T) {
	f := newFakeQuotes()
	cd := &countingCooldown{allow: false}
	out := runQuotes(t, engine.Deps{Quotes: f, Cooldown: cd}, quotesCtx("mod_amy", "moderator"), `"cooldown never gates saves"`)

	require.Len(t, out, 1)
	assert.Equal(t, 0, cd.claims)
}

func TestQuoteNilStoreInert(t *testing.T) {
	out := runQuotes(t, engine.Deps{}, quotesCtx("alice", ""), "")
	assert.Empty(t, out)
}
