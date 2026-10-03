// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"context"
	"sort"
	"strings"
	"testing"
	"time"

	"ItsBagelBot/app/twitch/sesame/engine"
	modulesrpc "ItsBagelBot/internal/domain/rpc/modules"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeQuotes struct {
	quotes map[uint64]modulesrpc.Quote
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
	var highest uint64
	for n := range f.quotes {
		highest = max(highest, n)
	}
	q := modulesrpc.Quote{Number: highest + 1, Text: text, AddedBy: addedBy, CreatedAt: "2026-07-11T10:00:00Z"}
	f.quotes[q.Number] = q
	return q, nil
}

func (f *fakeQuotes) QuoteGet(_ context.Context, _ uint64, number uint64) (modulesrpc.Quote, bool, error) {
	q, ok := f.quotes[number]
	return q, ok, nil
}

func (f *fakeQuotes) lowest(match func(modulesrpc.Quote) bool) (modulesrpc.Quote, bool, error) {
	var best modulesrpc.Quote
	found := false
	for _, q := range f.quotes {
		if !match(q) {
			continue
		}
		if !found || q.Number < best.Number {
			best, found = q, true
		}
	}
	return best, found, nil
}

func (f *fakeQuotes) QuoteRandom(context.Context, uint64) (modulesrpc.Quote, bool, error) {
	return f.lowest(func(modulesrpc.Quote) bool { return true })
}

func (f *fakeQuotes) QuoteSearch(_ context.Context, _ uint64, term string) (modulesrpc.Quote, bool, error) {
	return f.lowest(func(q modulesrpc.Quote) bool { return strings.Contains(strings.ToLower(q.Text), strings.ToLower(term)) })
}

func (f *fakeQuotes) QuoteEdit(_ context.Context, _ uint64, number uint64, text string) (modulesrpc.Quote, bool, error) {
	q, ok := f.quotes[number]
	if !ok {
		return modulesrpc.Quote{}, false, nil
	}
	q.Text = text
	f.quotes[number] = q
	return q, true, nil
}

func (f *fakeQuotes) QuoteRemove(_ context.Context, _ uint64, number uint64) (bool, error) {
	_, ok := f.quotes[number]
	delete(f.quotes, number)
	return ok, nil
}

type countingCooldown struct {
	engine.NoopCooldown
	allow  bool
	claims int
}

func (c *countingCooldown) Allow(context.Context, string, time.Duration) (bool, error) {
	c.claims++
	return c.allow, nil
}

func (f *fakeQuotes) orderedTexts() []string {
	numbers := make([]uint64, 0, len(f.quotes))
	for n := range f.quotes {
		numbers = append(numbers, n)
	}
	sort.Slice(numbers, func(i, j int) bool { return numbers[i] < numbers[j] })
	var got []string
	for _, n := range numbers {
		got = append(got, f.quotes[n].Text)
	}
	return got
}

func TestQuotes(t *testing.T) {
	ferret := []string{"never trust a ferret", "bagels are sentient"}
	denied := func() *countingCooldown { return &countingCooldown{} }
	cases := []struct {
		name     string
		text     string
		who      string
		badge    string
		config   string
		initial  []string
		cooldown *countingCooldown
		claims   int
		silent   bool
		exact    string
		contains []string
		want     []string
	}{
		{name: "a bare quote reads one at random", text: "!quote", who: "alice", initial: ferret[:1], exact: "Quote #1: never trust a ferret (2026-07-10)", want: ferret[:1]},
		{name: "a number reads that quote", text: "!quote 2", who: "alice", initial: []string{"one", "two"}, exact: "Quote #2: two (2026-07-10)", want: []string{"one", "two"}},
		{name: "a missing number says it does not exist", text: "!quote 7", who: "alice", initial: []string{"one"}, contains: []string{"#7", "doesn't exist"}, want: []string{"one"}},
		{name: "an empty book says so", text: "!quote", who: "alice", contains: []string{"No quotes saved yet"}},
		{name: "a word searches case insensitively", text: "!quote Ferret", who: "alice", initial: ferret, exact: "Quote #1: never trust a ferret (2026-07-10)", want: ferret},
		{name: "a multi word term searches the text", text: "!quote are sentient", who: "alice", initial: ferret, exact: "Quote #2: bagels are sentient (2026-07-10)", want: ferret},
		{name: "a search with no match says so", text: "!quote something funny", who: "alice", initial: []string{"one"}, contains: []string{`No quote matching "something funny"`}, want: []string{"one"}},
		{name: "a throttled search stays silent", text: "!quote one", who: "alice", initial: []string{"one"}, cooldown: denied(), claims: 1, silent: true, want: []string{"one"}},
		{name: "a throttled read stays silent", text: "!quote", who: "alice", initial: []string{"one"}, cooldown: denied(), claims: 1, silent: true, want: []string{"one"}},
		{name: "a mod saves a quoted quote", text: `!quote "the bagels are sentient"`, who: "mod_amy", badge: "moderator", initial: []string{"existing"},
			contains: []string{"#2", "added"}, want: []string{"existing", "the bagels are sentient"}},
		{name: "curly quotes unwrap too", text: "!quote “smart quotes too”", who: "mod_amy", badge: "moderator", contains: []string{"added"}, want: []string{"smart quotes too"}},
		{name: "the add subcommand takes plain words", text: "!quote add plain words work", who: "streamer", badge: "broadcaster", contains: []string{"added"}, want: []string{"plain words work"}},
		{name: "saving ignores the read cooldown", text: `!quote "cooldown never gates saves"`, who: "mod_amy", badge: "moderator", cooldown: denied(),
			contains: []string{"added"}, want: []string{"cooldown never gates saves"}},
		{name: "a viewer's save is silent", text: `!quote "nice try"`, who: "alice", silent: true},
		{name: "addPerm sub lets a sub save", text: `!quote "subs can save now"`, who: "subby", badge: "subscriber", config: `{"addPerm":"sub"}`,
			contains: []string{"added"}, want: []string{"subs can save now"}},
		{name: "addPerm sub still blocks a viewer", text: `!quote "no badge here"`, who: "alice", config: `{"addPerm":"sub"}`, silent: true},
		{name: "addPerm everyone lets a viewer save", text: `!quote "anyone can save"`, who: "alice", config: `{"addPerm":"everyone"}`,
			contains: []string{"added"}, want: []string{"anyone can save"}},
		{name: "quoteadd saves plain text", text: "!quoteadd no quoting needed here", who: "mod_amy", badge: "moderator", contains: []string{"added"}, want: []string{"no quoting needed here"}},
		{name: "quoteadd still unwraps a quoted body", text: `!quoteadd "still unwraps"`, who: "mod_amy", badge: "moderator", contains: []string{"added"}, want: []string{"still unwraps"}},
		{name: "the addquote alias saves", text: "!addquote via the alias", who: "mod_amy", badge: "moderator", contains: []string{"added"}, want: []string{"via the alias"}},
		{name: "a viewer's quoteadd is silent", text: "!quoteadd nice try", who: "alice", silent: true},
		{name: "quoteadd honours addPerm", text: "!quoteadd anyone can save", who: "alice", config: `{"addPerm":"everyone"}`, contains: []string{"added"}, want: []string{"anyone can save"}},
		{name: "a mod removes a quote", text: "!quote remove 1", who: "mod_amy", badge: "moderator", initial: []string{"one", "two"}, contains: []string{"#1", "removed"}, want: []string{"two"}},
		{name: "a viewer's remove is silent", text: "!quote remove 1", who: "alice", initial: []string{"one"}, silent: true, want: []string{"one"}},
		{name: "remove ignores addPerm", text: "!quote remove 1", who: "alice", config: `{"addPerm":"everyone"}`, initial: []string{"one"}, silent: true, want: []string{"one"}},
		{name: "remove without a number prints usage", text: "!quote remove ferret", who: "mod_amy", badge: "moderator", initial: []string{"one"}, contains: []string{"Usage"}, want: []string{"one"}},
		{name: "a mod edits a quote", text: "!quote edit 1 the bagels", who: "mod_amy", badge: "moderator", initial: []string{"teh bagels"}, contains: []string{"#1", "updated"}, want: []string{"the bagels"}},
		{name: "an edit unwraps a quoted body", text: `!quote edit 1 "new text"`, who: "mod_amy", badge: "moderator", initial: []string{"old"}, contains: []string{"updated"}, want: []string{"new text"}},
		{name: "editing a missing number says so", text: "!quote edit 7 rewritten", who: "mod_amy", badge: "moderator", initial: []string{"one"}, contains: []string{"doesn't exist"}, want: []string{"one"}},
		{name: "a malformed edit prints usage", text: "!quote edit ferret words", who: "mod_amy", badge: "moderator", initial: []string{"one"}, contains: []string{"Usage"}, want: []string{"one"}},
		{name: "a viewer's edit is silent", text: "!quote edit 1 hijacked", who: "alice", initial: []string{"one"}, silent: true, want: []string{"one"}},
		{name: "editPerm vip lets a vip edit", text: "!quote edit 1 vips can edit", who: "vippy", badge: "vip", config: `{"editPerm":"vip"}`, initial: []string{"old"},
			contains: []string{"updated"}, want: []string{"vips can edit"}},
		{name: "editPerm vip still blocks a viewer", text: "!quote edit 1 nope", who: "alice", config: `{"editPerm":"vip"}`, initial: []string{"old"}, silent: true, want: []string{"old"}},
		{name: "edit ignores addPerm", text: "!quote edit 1 sneaky", who: "alice", config: `{"addPerm":"everyone"}`, initial: []string{"old"}, silent: true, want: []string{"old"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeQuotes(tc.initial...)
			d := engine.Deps{Quotes: f}
			if tc.cooldown != nil {
				d.Cooldown = tc.cooldown
			}
			out := runChat(t, Quotes(d), withConfig(chatCtx("42", tc.who, tc.badge), tc.config), tc.text)
			if tc.silent {
				assert.Empty(t, out)
			} else {
				require.Len(t, out, 1)
				assertText(t, out[0].Text, textWant{tc.exact, tc.contains, nil})
			}
			assert.Equal(t, tc.want, f.orderedTexts())
			if tc.cooldown != nil {
				assert.Equal(t, tc.claims, tc.cooldown.claims)
			}
		})
	}
}

func TestQuoteAddRecordsTheAdder(t *testing.T) {
	f := newFakeQuotes()
	runChat(t, Quotes(engine.Deps{Quotes: f}), chatCtx("42", "mod_amy", "moderator"), "!quoteadd hello")
	assert.Equal(t, "mod_amy", f.quotes[1].AddedBy)
}

func TestQuotesStayInertWithoutAStore(t *testing.T) {
	assert.Empty(t, runChat(t, Quotes(engine.Deps{}), chatCtx("42", "alice"), "!quote"))
}
