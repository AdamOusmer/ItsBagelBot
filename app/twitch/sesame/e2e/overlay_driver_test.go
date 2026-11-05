// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package e2e

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	modulesent "ItsBagelBot/app/db/modules/ent"
	"ItsBagelBot/app/db/modules/ent/enttest"
	modulesrepo "ItsBagelBot/app/db/modules/repository"
	modulesserver "ItsBagelBot/app/db/modules/rpc"
	"ItsBagelBot/app/twitch/sesame/engine"
	"ItsBagelBot/app/twitch/sesame/modules"
	"ItsBagelBot/internal/domain/automation"
	"ItsBagelBot/internal/domain/event/lane"
	"ItsBagelBot/internal/testdb"
	"ItsBagelBot/internal/valkeytest"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/bus/bustest"
	"ItsBagelBot/pkg/codec"

	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

const overlayRecipe = `{
  "version": 1,
  "trigger": { "type": "command", "params": { "name": "!show" } },
  "actions": [
    { "type": "overlay_show", "params": { "kind": "text", "content": "{args}", "seconds": 2, "position": "center" } },
    { "type": "overlay_show", "params": { "kind": "image", "content": "https://assets.e2e.test/one.png", "seconds": 2, "position": "top" } },
    { "type": "overlay_sound", "params": { "url": "https://assets.e2e.test/two.wav", "volume": 40 } },
    { "type": "overlay_show", "params": { "kind": "image", "content": "https://assets.e2e.test/three.png", "seconds": 1, "position": "bottom" } }
  ]
}`

const overlayTextRecipe = `{
  "version": 1,
  "trigger": { "type": "command", "params": { "name": "!say" } },
  "actions": [
    { "type": "overlay_show", "params": { "kind": "text", "content": "{args}", "seconds": 1, "position": "bottom" } }
  ]
}`

func overlayAutomation(t *testing.T, id, body string, broadcaster uint64) automation.Automation {
	t.Helper()
	var def automation.Definition
	require.NoError(t, codec.UnmarshalFromString(body, &def))
	require.Empty(t, automation.Validate(def))
	return automation.Automation{ID: id, UserID: broadcaster, Name: id, Enabled: true, Definition: def}
}

// Runs real engine recipes against the Valkey the production dashboard reads, on commands from a scratch browser driver.
func TestRecipesV2OverlayDriver(t *testing.T) {
	if os.Getenv("RECIPES_E2E_OVERLAY_DRIVER") == "" {
		t.Skip("RECIPES_E2E_OVERLAY_DRIVER is not set")
	}
	vc := valkeytest.Real(t)
	natsURL := startNATS(t)
	nc, err := nats.Connect(natsURL)
	require.NoError(t, err)
	defer nc.Close()

	client := testdb.Open(t, testdb.Name(t.Name()), func(d, dsn string) *modulesent.Client { return enttest.Open(t, d, dsn) })
	repo := modulesrepo.NewModules(client, bustest.NewPublisher(), nil, zap.NewNop())
	defer repo.Close(context.Background())
	require.NoError(t, modulesserver.SubscribeDashboard(modulesserver.Wiring{RPCWiring: bus.RPCWiring{NC: nc, Log: zap.NewNop()}, Repo: repo}, os.Getenv("RECIPES_E2E_MODULES_PREFIX")))

	source := &fixtureSource{autos: map[uint64][]automation.Automation{}}
	runs := engine.NewValkeyRunLog(vc, zap.NewNop())
	hook := &engine.AutomationHook{}
	pub := bustest.NewPublisher()
	live := engine.NewValkeyLiveStore(vc, nc, pub, engine.LiveConfig{TTL: time.Hour, CacheTTL: time.Second, Log: zap.NewNop()})
	defer live.Close()
	d := engine.Deps{
		AutomationHook: hook,
		Automations:    engine.AutomationDeps{Source: source, Runs: runs, Overlay: engine.NewValkeyOverlay(vc), Queue: engine.NewValkeyRunQueue(vc)},
		Proj:           premiumChannels{}, Live: live, Cooldown: engine.NoopCooldown{}, Special: engine.NewSpecialSet(""),
		Greet: engine.NewValkeyGreetStore(vc, time.Hour, zap.NewNop()),
		Pub:   pub, Log: zap.NewNop(), Seq: engine.NewSequencer(),
	}
	pipe := engine.NewPipeline(d, engine.NewRegistry(zap.NewNop(), modules.All(d)...), engine.Config{OutgressPremium: premiumSubject, OutgressStandard: standardSubject})
	hook.Wire(pipe)
	defer pipe.Close()

	fmt.Printf("RECIPES_E2E_READY %s\n", natsURL)
	events := 0
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 64*1024), 64*1024)
	for scanner.Scan() {
		parts := strings.SplitN(scanner.Text(), " ", 2)
		broadcaster, err := strconv.ParseUint(parts[0], 10, 64)
		require.NoError(t, err)
		source.set(broadcaster, overlayAutomation(t, "overlay", overlayRecipe, broadcaster), overlayAutomation(t, "say", overlayTextRecipe, broadcaster))
		events++
		env := lane.Envelope{
			Type: "channel.chat.message", Lane: "premium", EventID: fmt.Sprintf("overlay-%d-%d", os.Getpid(), events), Text: parts[1],
			BroadcasterUserID: parts[0], BroadcasterUserLogin: "streamer", ChatterUserID: "777", ChatterUserLogin: "viewer",
		}
		body, err := codec.Marshal(env)
		require.NoError(t, err)
		require.NoError(t, pipe.Process(bus.NewMessage(env.EventID, body)))
		id := "overlay"
		if strings.HasPrefix(parts[1], "!say") {
			id = "say"
		}
		fmt.Println("RESULT", awaitRunJSON(t, runs, runLookup{Broadcaster: broadcaster, ID: id, Event: events}))
	}
}

type runLookup struct {
	Broadcaster uint64
	ID          string
	Event       int
}

func awaitRunJSON(t *testing.T, runs *engine.ValkeyRunLog, q runLookup) string {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		recent, err := runs.Recent(context.Background(), q.Broadcaster, q.ID, 1)
		require.NoError(t, err)
		if len(recent) > 0 && time.Since(recent[0].At) < 3*time.Second {
			out, err := json.Marshal(map[string]any{"event": q.Event, "outcome": recent[0].Outcome, "steps": stepStatuses(recent[0])})
			require.NoError(t, err)
			return string(out)
		}
		time.Sleep(20 * time.Millisecond)
	}
	return `{"outcome":"none"}`
}
