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

func TestBuiltinCommandsKeepViewerTokensLiteral(t *testing.T) {
	const probe = "{urlfetch:probe.secret}"
	nop := engine.Deps{Log: zap.NewNop(), Special: engine.NewSpecialSet("")}
	for _, m := range All(nop) {
		for _, cmd := range m.Commands {
			for _, args := range []string{probe, "add " + probe, "x " + probe, "@viewer " + probe} {
				t.Run(cmd.Name+"/"+args, func(t *testing.T) {
					fetch, out := runBuiltin(t, m.Name, "!"+cmd.Name+" "+args)
					if fetch.count() != 0 {
						t.Fatalf("viewer token ran a fetch; replies %q", out)
					}
					for _, line := range out {
						if strings.Contains(line, "LEAKED") {
							t.Fatalf("viewer token resolved in reply %q", line)
						}
					}
				})
			}
		}
	}
}

func TestStoredQuoteKeepsTokensLiteral(t *testing.T) {
	fetch, out := runBuiltin(t, "quotes", "!quote 1")
	if fetch.count() != 0 || len(out) != 1 || !strings.Contains(out[0], "{urlfetch:probe.secret}") {
		t.Fatalf("fetches=%d replies=%q", fetch.count(), out)
	}
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
		Quotes:      newFakeQuotes("said {urlfetch:probe.secret}"),
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
