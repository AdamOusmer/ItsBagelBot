// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"context"
	"strings"
	"sync"
	"testing"

	"ItsBagelBot/app/twitch/sesame/engine"
	gossiprpc "ItsBagelBot/internal/domain/rpc/gossip"
	"ItsBagelBot/internal/projection"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/codec"

	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

type probeFetch struct {
	mu    sync.Mutex
	calls int
}

func (f *probeFetch) Fetch(context.Context, gossiprpc.Request) (gossiprpc.CustomFetchReply, error) {
	f.mu.Lock()
	f.calls++
	f.mu.Unlock()
	return gossiprpc.CustomFetchReply{Status: gossiprpc.FetchOK, Values: []string{"LEAKED"}}, nil
}

func (f *probeFetch) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

const probeToken = "{urlfetch:probe.secret}"

type builtinProbe struct {
	module string
	text   string
}

func builtinProbes() []builtinProbe {
	nop := engine.Deps{Log: zap.NewNop(), Special: engine.NewSpecialSet("")}
	var probes []builtinProbe
	for _, m := range All(nop) {
		for _, cmd := range m.Commands {
			for _, prefix := range []string{"", "add ", "x ", "@viewer "} {
				probes = append(probes, builtinProbe{module: m.Name, text: "!" + cmd.Name + " " + prefix + probeToken})
			}
		}
	}
	return probes
}

func TestBuiltinCommandsKeepViewerTokensLiteral(t *testing.T) {
	for _, probe := range builtinProbes() {
		t.Run(probe.text, func(t *testing.T) {
			fetch, out := runBuiltin(t, probe.module, probe.text)
			assert.Zero(t, fetch.count(), "viewer token ran a fetch; replies %q", out)
			assert.NotContains(t, strings.Join(out, "\n"), "LEAKED")
		})
	}
}

func TestStoredQuoteKeepsTokensLiteral(t *testing.T) {
	fetch, out := runBuiltin(t, "quotes", "!quote 1")
	assert.Zero(t, fetch.count())
	assert.Equal(t, []string{"Quote #1: said " + probeToken + " (2026-07-10)"}, out)
}

func runBuiltin(t *testing.T, moduleName, text string) (*probeFetch, []string) {
	t.Helper()
	pub := &variablePublisher{}
	fetch := &probeFetch{}
	views := []projection.ModuleView{{Name: moduleName, IsEnabled: true, Configs: []byte(`{}`)}}
	d := engine.Deps{
		Proj:        &fakeProj{commands: map[string]projection.Command{}, modules: views},
		Gossip:      &fakeGossip{},
		Pub:         pub,
		Live:        &fakeLive{live: true},
		Greet:       &fakeGreet{},
		Special:     engine.NewSpecialSet(""),
		Cooldown:    engine.NoopCooldown{},
		Log:         zap.NewNop(),
		Quotes:      newFakeQuotes("said " + probeToken),
		Followage:   &fakeFollowage{},
		CustomFetch: fetch,
	}
	p := engine.NewPipeline(d, engine.NewRegistry(d.Log, All(d)...), engine.Config{OutgressStandard: "outgress.standard"})
	defer p.Close()
	body, err := codec.Marshal(map[string]string{
		"type": "channel.chat.message", "lane": "standard",
		"broadcaster_user_id": "2", "broadcaster_user_login": "streamer",
		"chatter_user_id": "2", "chatter_user_login": "streamer",
		"text": text,
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = p.Process(bus.NewMessage("probe", body))
	var out []string
	for _, m := range pub.messages {
		var payload struct {
			Message string `json:"message"`
		}
		_ = codec.Unmarshal(m.Payload, &payload)
		out = append(out, payload.Message)
	}
	return fetch, out
}
