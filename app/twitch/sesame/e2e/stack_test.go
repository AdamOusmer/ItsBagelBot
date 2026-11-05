// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package e2e

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"ItsBagelBot/app/db/loyalty/ent"
	_ "ItsBagelBot/app/db/loyalty/ent/runtime"
	loyaltyrepo "ItsBagelBot/app/db/loyalty/repository"
	loyaltyserver "ItsBagelBot/app/db/loyalty/rpc"
	"ItsBagelBot/app/twitch/sesame/engine"
	"ItsBagelBot/app/twitch/sesame/modules"
	"ItsBagelBot/internal/domain/automation"
	"ItsBagelBot/internal/domain/event/lane"
	"ItsBagelBot/internal/projection"
	"ItsBagelBot/internal/testdb"
	"ItsBagelBot/internal/valkeytest"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/codec"
	"ItsBagelBot/pkg/idempotency"

	entsql "entgo.io/ent/dialect/sql"
	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/require"
	"github.com/valkey-io/valkey-go"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
)

const (
	gossipPrefix    = "bagel.rpc.gossip"
	loyaltyPrefix   = "loyalty"
	premiumSubject  = "twitch.outgress.premium"
	standardSubject = "twitch.outgress.standard"
	upstreamHost    = "upstream.e2e.test"
	upstreamCertSNI = "example.com"
	fixtureDir      = "../../../../internal/domain/automation/testdata/valid"
	repoRoot        = "../../../.."
)

type helixCall struct {
	At          time.Time
	Method      string
	Path        string
	Broadcaster string
	RewardID    string
	Body        string
}

type rewardState struct {
	Enabled bool
	Cost    int
}

// Stops at the Twitch boundary: everything before this handler is the real outgress worker over real JetStream.
type fakeHelix struct {
	mu      sync.Mutex
	calls   []helixCall
	rewards map[rewardRef]*rewardState
	foreign map[rewardRef]bool
}

type rewardRef struct {
	Broadcaster uint64
	ID          string
}

func (h *fakeHelix) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	query := r.URL.Query()
	call := helixCall{At: time.Now(), Method: r.Method, Path: r.URL.Path, Broadcaster: query.Get("broadcaster_id"), RewardID: query.Get("id"), Body: string(body)}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.calls = append(h.calls, call)
	w.Header().Set("Content-Type", "application/json")
	switch {
	case r.URL.Path == "/helix/chat/messages":
		_, _ = w.Write([]byte(`{"data":[{"message_id":"m","is_sent":true}]}`))
	case strings.HasSuffix(r.URL.Path, "/custom_rewards"):
		h.serveReward(w, r.Method, call)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

func (h *fakeHelix) serveReward(w http.ResponseWriter, method string, call helixCall) {
	if h.foreign[call.rewardRef()] {
		w.WriteHeader(http.StatusForbidden)
		return
	}
	reward := h.rewards[call.rewardRef()]
	if reward == nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	if method == http.MethodPatch {
		var patch struct {
			IsEnabled *bool `json:"is_enabled"`
			Cost      *int  `json:"cost"`
		}
		_ = codec.UnmarshalFromString(call.Body, &patch)
		if patch.IsEnabled != nil {
			reward.Enabled = *patch.IsEnabled
		}
		if patch.Cost != nil {
			reward.Cost = *patch.Cost
		}
	}
	_, _ = fmt.Fprintf(w, `{"data":[{"id":%q,"title":"Hydrate","prompt":"","cost":%d,"is_enabled":%t}]}`, call.RewardID, reward.Cost, reward.Enabled)
}

func (h *fakeHelix) seedReward(ref rewardRef, state rewardState) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.rewards[ref] = &state
}

func (h *fakeHelix) seedForeignReward(ref rewardRef) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.foreign[ref] = true
}

func (h *fakeHelix) reward(ref rewardRef) rewardState {
	h.mu.Lock()
	defer h.mu.Unlock()
	return *h.rewards[ref]
}

func (h *fakeHelix) snapshot() []helixCall {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]helixCall(nil), h.calls...)
}

type chatLine struct {
	At   time.Time
	Text string
}

func (h *fakeHelix) chatLines(t *testing.T, broadcaster uint64) []chatLine {
	t.Helper()
	id := strconv.FormatUint(broadcaster, 10)
	var lines []chatLine
	for _, c := range h.snapshot() {
		if c.Path != "/helix/chat/messages" {
			continue
		}
		var body struct {
			BroadcasterID string `json:"broadcaster_id"`
			Message       string `json:"message"`
		}
		require.NoError(t, codec.UnmarshalFromString(c.Body, &body))
		if body.BroadcasterID == id {
			lines = append(lines, chatLine{c.At, body.Message})
		}
	}
	return lines
}

func (h *fakeHelix) patches(broadcaster uint64) []helixCall {
	id := strconv.FormatUint(broadcaster, 10)
	var out []helixCall
	for _, c := range h.snapshot() {
		if c.Method == http.MethodPatch && c.Broadcaster == id {
			out = append(out, c)
		}
	}
	return out
}

type upstreamHit struct {
	At   time.Time
	Path string
}

type upstreamLog struct {
	mu   sync.Mutex
	hits []upstreamHit
}

func (u *upstreamLog) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	u.mu.Lock()
	u.hits = append(u.hits, upstreamHit{time.Now(), r.URL.Path})
	u.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	if r.URL.Path == "/item/2" {
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}
	_, _ = w.Write([]byte(`{"data":{"value":"from-upstream"}}`))
}

func (u *upstreamLog) snapshot() []upstreamHit {
	u.mu.Lock()
	defer u.mu.Unlock()
	return append([]upstreamHit(nil), u.hits...)
}

// Only the module switch and paid flag are stubbed: they come from a SQL projection that is not under test.
type premiumChannels struct{}

func (premiumChannels) User(context.Context, uint64) (projection.User, error) {
	return projection.User{IsActive: true, Status: "premium"}, nil
}

func (premiumChannels) Modules(context.Context, uint64) (map[string]projection.ModuleView, error) {
	return map[string]projection.ModuleView{engine.AutomationsModuleName: {Name: engine.AutomationsModuleName, IsEnabled: true}}, nil
}

func (r premiumChannels) Module(ctx context.Context, id uint64, name string) (projection.ModuleView, bool, error) {
	views, _ := r.Modules(ctx, id)
	view, ok := views[name]
	return view, ok, nil
}

func (premiumChannels) Command(context.Context, uint64, string) (projection.Command, bool, error) {
	return projection.Command{}, false, nil
}

type stack struct {
	vc       valkey.Client
	nc       *nats.Conn
	pub      bus.Publisher
	natsURL  string
	vars     *engine.LoyaltyRPC
	helix    *fakeHelix
	upstream *upstreamLog
	runs     *engine.ValkeyRunLog
	source   *fixtureSource
	replicas [2]*engine.Pipeline
	events   int
	mu       sync.Mutex
}

type fixtureSource struct {
	mu    sync.Mutex
	autos map[uint64][]automation.Automation
}

func (s *fixtureSource) set(broadcaster uint64, autos ...automation.Automation) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.autos[broadcaster] = autos
}

func (s *fixtureSource) Automations(_ context.Context, broadcaster uint64) ([]automation.Automation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.autos[broadcaster], nil
}

func (s *fixtureSource) AutomationIndex(ctx context.Context, broadcaster uint64) (*automation.TriggerIndex, error) {
	autos, err := s.Automations(ctx, broadcaster)
	return automation.CompileIndex(autos), err
}

func startNATS(t *testing.T) string {
	t.Helper()
	s, err := server.NewServer(&server.Options{Host: "127.0.0.1", Port: -1, JetStream: true, JetStreamDomain: bus.JSDomain(), StoreDir: t.TempDir(), NoLog: true, NoSigs: true})
	require.NoError(t, err)
	s.Start()
	t.Cleanup(func() {
		s.Shutdown()
		s.WaitForShutdown()
	})
	require.True(t, s.ReadyForConnections(10*time.Second))
	return s.ClientURL()
}

func startLoyalty(t *testing.T, nc *nats.Conn) {
	t.Helper()
	db, err := sql.Open(testdb.Driver, testdb.MemDSN(testdb.Name(t.Name())))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	drv := entsql.OpenDB("sqlite3", db)
	client := ent.NewClient(ent.Driver(drv))
	db.SetMaxOpenConns(1)
	require.NoError(t, loyaltyrepo.MigrateSchema(context.Background(), client, "sqlite3"))
	repo := loyaltyrepo.NewLoyalty(client, drv, nil, zap.NewNop())
	t.Cleanup(func() { repo.Close(context.Background()) })
	require.NoError(t, loyaltyserver.Subscribe(loyaltyserver.Wiring{RPCWiring: bus.RPCWiring{NC: nc, Log: zaptest.NewLogger(t, zaptest.Level(zap.WarnLevel))}, Repo: repo, InvalidationPrefix: "bagel.cache.invalidate"}, loyaltyPrefix))
}

type serviceBuild struct{ Pkg, Name string }

func buildService(t *testing.T, svc serviceBuild) string {
	t.Helper()
	binary := filepath.Join(t.TempDir(), svc.Name)
	build := exec.Command("go", "test", "-c", "-o", binary, svc.Pkg)
	build.Dir = repoRoot
	out, err := build.CombinedOutput()
	require.NoError(t, err, string(out))
	return binary
}

// A separate process per service: gossip's and outgress's packages are internal, and this is how they run anyway.
func startService(t *testing.T, binary string, env ...string) {
	t.Helper()
	cmd := exec.Command(binary, "-test.run", "^TestRecipesE2EServe$", "-test.timeout", "20m")
	cmd.Env = append(os.Environ(), env...)
	stdin, err := cmd.StdinPipe()
	require.NoError(t, err)
	stdout, err := cmd.StdoutPipe()
	require.NoError(t, err)
	cmd.Stderr = os.Stderr
	require.NoError(t, cmd.Start())
	t.Cleanup(func() {
		_ = stdin.Close()
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})
	ready := make(chan struct{})
	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			if strings.Contains(scanner.Text(), "RECIPES_E2E_READY") {
				close(ready)
				break
			}
		}
		_, _ = io.Copy(io.Discard, stdout)
	}()
	select {
	case <-ready:
	case <-time.After(30 * time.Second):
		t.Fatalf("%s did not become ready", filepath.Base(binary))
	}
}

func ensureOutgressStreams(t *testing.T, natsURL string) {
	t.Helper()
	premium, system := bus.OutgressStream, bus.OutgressSystemStream
	premium.Replicas, system.Replicas = 1, 1
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	require.NoError(t, bus.EnsureStreams(ctx, natsURL, []bus.StreamSpec{premium, system}, zaptest.NewLogger(t, zaptest.Level(zap.WarnLevel))))
}

func startStack(t *testing.T, channels ...uint64) *stack {
	t.Helper()
	addr := os.Getenv("VALKEY_TEST_ADDR")
	if addr == "" {
		t.Skip("VALKEY_TEST_ADDR is not set")
	}
	vc := valkeytest.Real(t)
	natsURL := startNATS(t)
	nc, err := nats.Connect(natsURL)
	require.NoError(t, err)
	t.Cleanup(nc.Close)
	ensureOutgressStreams(t, natsURL)
	startLoyalty(t, nc)

	s := &stack{vc: vc, nc: nc, natsURL: natsURL, helix: &fakeHelix{rewards: map[rewardRef]*rewardState{}, foreign: map[rewardRef]bool{}}, upstream: &upstreamLog{}, source: &fixtureSource{autos: map[uint64][]automation.Automation{}}}
	s.vars = engine.NewLoyaltyRPC(nc, engine.LoyaltyRPCPrefix(loyaltyPrefix))
	s.runs = engine.NewValkeyRunLog(vc, zap.NewNop())

	helixServer := httptest.NewServer(s.helix)
	t.Cleanup(helixServer.Close)
	upstreamServer := httptest.NewTLSServer(s.upstream)
	t.Cleanup(upstreamServer.Close)
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: upstreamServer.Certificate().Raw})

	common := []string{"RECIPES_E2E_NATS_URL=" + natsURL, "VALKEY_TEST_ADDR=" + addr}
	var wg sync.WaitGroup
	var gossipBin, outgressBin string
	wg.Add(2)
	go func() {
		defer wg.Done()
		gossipBin = buildService(t, serviceBuild{"./app/gossip/internal/core", "gossip-e2e"})
	}()
	go func() {
		defer wg.Done()
		outgressBin = buildService(t, serviceBuild{"./app/twitch/outgress/internal/worker", "outgress-e2e"})
	}()
	wg.Wait()
	startService(t, gossipBin, append(common,
		"RECIPES_E2E_GOSSIP_PREFIX="+gossipPrefix,
		"RECIPES_E2E_UPSTREAM_HOST="+upstreamHost,
		"RECIPES_E2E_UPSTREAM_ADDR="+strings.TrimPrefix(upstreamServer.URL, "https://"),
		"RECIPES_E2E_UPSTREAM_SNI="+upstreamCertSNI,
		"RECIPES_E2E_UPSTREAM_CERT="+string(certPEM))...)
	ids := make([]string, len(channels))
	for i, id := range channels {
		ids[i] = strconv.FormatUint(id, 10)
	}
	startService(t, outgressBin, append(common,
		"RECIPES_E2E_TWITCH_URL="+helixServer.URL,
		"RECIPES_E2E_CHANNELS="+strings.Join(ids, ","))...)

	s.pub, err = bus.NewPublisher(natsURL, zap.NewNop())
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.pub.Close() })
	for i := range s.replicas {
		s.replicas[i] = s.replica(t)
	}
	return s
}

func (s *stack) replica(t *testing.T) *engine.Pipeline {
	t.Helper()
	log := zap.NewNop()
	gossip := engine.NewGossipRPC(s.nc, gossipPrefix)
	live := engine.NewValkeyLiveStore(s.vc, s.nc, s.pub, engine.LiveConfig{TTL: time.Hour, CacheTTL: time.Second, Log: log})
	t.Cleanup(live.Close)
	d := engine.Deps{
		AutomationHook: &engine.AutomationHook{},
		Automations: engine.AutomationDeps{
			Source: s.source, Runs: s.runs, Limiter: engine.NewValkeyRunLimiter(s.vc),
			Variables: s.vars, Overlay: engine.NewValkeyOverlay(s.vc), Queue: engine.NewValkeyRunQueue(s.vc),
		},
		Proj: premiumChannels{}, Live: live, RewardResets: engine.NewValkeyRewardResets(s.vc),
		Greet: engine.NewValkeyGreetStore(s.vc, time.Hour, log), Cooldown: engine.NoopCooldown{},
		Special: engine.NewSpecialSet(""), Pub: s.pub, Gossip: gossip, CustomFetch: gossip, Log: log,
		Dedup: engine.NewEventDedup(idempotency.NewTiered(1000, idempotency.NewValkeyStore(s.vc, "e2e:seen:", log)), "e2e:seen:", time.Hour, log),
		Seq:   engine.NewSequencer(),
	}
	pipe := engine.NewPipeline(d, engine.NewRegistry(log, modules.All(d)...), engine.Config{BotID: "987654", OutgressPremium: premiumSubject, OutgressStandard: standardSubject})
	d.AutomationHook.Wire(pipe)
	t.Cleanup(pipe.Close)
	return pipe
}

func (s *stack) nextEvent(prefix string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events++
	return fmt.Sprintf("%s-%d-%d", prefix, time.Now().UnixNano(), s.events)
}

type chatCommand struct {
	Replica     int
	Broadcaster uint64
	Viewer      string
	Text        string
}

func (s *stack) command(t *testing.T, c chatCommand) {
	t.Helper()
	env := lane.Envelope{
		Type: "channel.chat.message", Lane: "premium", EventID: s.nextEvent("chat"), Text: c.Text,
		BroadcasterUserID: strconv.FormatUint(c.Broadcaster, 10), BroadcasterUserLogin: "streamer",
		ChatterUserID: "777", ChatterUserLogin: c.Viewer,
	}
	s.process(t, c.Replica, env)
}

func (s *stack) streamOffline(t *testing.T, broadcaster uint64) {
	t.Helper()
	s.process(t, 0, lane.Envelope{
		Type: "stream.offline", Lane: "premium", EventID: s.nextEvent("offline"),
		BroadcasterUserID: strconv.FormatUint(broadcaster, 10), BroadcasterUserLogin: "streamer",
	})
}

func (s *stack) process(t *testing.T, replica int, env lane.Envelope) {
	t.Helper()
	body, err := codec.Marshal(env)
	require.NoError(t, err)
	require.NoError(t, s.replicas[replica].Process(bus.NewMessage(env.EventID, body)))
}

func loadAutomation(t *testing.T, fixture string, broadcaster uint64) automation.Automation {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(fixtureDir, fixture))
	require.NoError(t, err)
	var def automation.Definition
	require.NoError(t, codec.Unmarshal(raw, &def))
	require.Empty(t, automation.Validate(def), "%s must validate", fixture)
	return automation.Automation{ID: strings.TrimSuffix(fixture, ".json"), UserID: broadcaster, Name: fixture, Enabled: true, Definition: def}
}

func (c helixCall) rewardRef() rewardRef {
	broadcaster, _ := strconv.ParseUint(c.Broadcaster, 10, 64)
	return rewardRef{Broadcaster: broadcaster, ID: c.RewardID}
}
