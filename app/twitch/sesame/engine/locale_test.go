// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"testing"
	"time"

	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/internal/projection"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type countingChannelReader struct {
	countingReader
	loads       int
	needModules bool
}

func (r *countingChannelReader) LoadChannel(_ context.Context, _ uint64, needModules bool) (map[string]projection.ModuleView, projection.User, error) {
	r.loads++
	r.needModules = needModules
	return r.modules, r.user, r.modErr
}

func localePing() module.Module {
	m := module.NewModule("", module.KindCore)
	m.Command("ping").Everyone().Run(func(ctx context.Context, c *module.Context, _ string, emit module.Emit) error {
		c.EnsureLocale(ctx)
		emit(&module.Output{Type: "chat", BroadcasterID: c.Env.BroadcasterUserID, Text: "locale=" + c.Locale})
		return nil
	})
	m.On(chatType, func(ctx context.Context, c *module.Context, _ module.Emit) error {
		if c.Command != "" {
			c.EnsureLocale(ctx)
		}
		return nil
	})
	return m.Build()
}

func silentVoice() module.Module {
	m := module.NewModule("voice", module.KindDefault)
	m.On(chatType, func(context.Context, *module.Context, module.Emit) error { return nil })
	return m.Build()
}

func TestLocaleIsReadOnlyWhenAHandlerNeedsIt(t *testing.T) {
	cases := []struct {
		name      string
		text      string
		locale    string
		module    module.Module
		wantReads int
		want      []published
	}{
		{name: "plain chat reads no locale", text: "hello", module: silentVoice()},
		{
			name: "a baked command loads the default locale once", text: "!ping", module: localePing(), wantReads: 1,
			want: []published{{standardSubj, "chat", "", "locale="}},
		},
		{
			name: "a baked command loads a configured locale once", text: "!ping", locale: "fr", module: localePing(), wantReads: 1,
			want: []published{{standardSubj, "chat", "", "locale=fr"}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reader := &countingReader{fakeReader: fakeReader{user: projection.User{Locale: tc.locale}}}
			pub := &fakePublisher{}

			require.NoError(t, newPipelineWith(pub, reader, tc.module).Process(chatMsg(t, "standard", tc.text)))

			assert.Equal(t, tc.wantReads, reader.userReads)
			assert.Equal(t, tc.want, publishedMessages(pub))
			for _, c := range pub.snapshot() {
				assert.Equal(t, tc.locale, c.msg.Locale, "the output carries the loaded locale")
			}
		})
	}
}

func TestBakedCommandUsesOptionalChannelLoader(t *testing.T) {
	reader := &countingChannelReader{countingReader: countingReader{fakeReader: fakeReader{user: projection.User{Locale: "fr"}}}}
	pub := &fakePublisher{}
	named := module.NewModule("named", module.KindDefault)
	named.Command("named").Everyone().Run(func(context.Context, *module.Context, string, module.Emit) error { return nil })
	p := newPipelineWith(pub, reader)
	p.registry = NewRegistry(p.log, localePing(), named.Build())

	require.NoError(t, p.Process(chatMsg(t, "standard", "!ping")))

	assert.Equal(t, 1, reader.loads)
	assert.True(t, reader.needModules, "a named command owner makes the registry require modules on chat")
	assert.Zero(t, reader.userReads)
	assert.Equal(t, []string{"locale=fr"}, pub.chatTexts(t))
}

type coldLocaleObserver struct{ events chan ObservedEvent }

func (o coldLocaleObserver) Observe(ev ObservedEvent) { o.events <- ev }

func TestGatedCommandObserverKeepsLocale(t *testing.T) {
	m := module.NewModule("", module.KindCore)
	m.Command("private").AllowUser("777").Run(func(context.Context, *module.Context, string, module.Emit) error {
		t.Fatal("permission denied body must not run")
		return nil
	})
	reader := &countingReader{fakeReader: fakeReader{user: projection.User{Locale: "fr"}}}
	p := newPipelineWith(&fakePublisher{}, reader, m.Build())
	events := make(chan ObservedEvent, 1)
	p.RegisterObserver(coldLocaleObserver{events: events})
	defer p.Close()

	require.NoError(t, p.Process(chatMsg(t, "standard", "!private")))

	select {
	case event := <-events:
		assert.Equal(t, "fr", event.Locale)
		assert.Equal(t, "private", event.Command)
	case <-time.After(time.Second):
		t.Fatal("missing command observation")
	}
	assert.Equal(t, 1, reader.userReads)
}
