// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package worker

import (
	"context"
	"io"
	"net/http"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"ItsBagelBot/app/twitch/outgress/internal/channels"
	"ItsBagelBot/app/twitch/outgress/internal/tokenstore"
	"ItsBagelBot/app/twitch/outgress/internal/twitch"
	"ItsBagelBot/internal/channelruntime"
	"ItsBagelBot/internal/domain/i18n"
	"ItsBagelBot/internal/domain/lifecycle"
	"ItsBagelBot/internal/domain/rpc"
	"ItsBagelBot/internal/domain/rpc/manage"
	"ItsBagelBot/internal/domain/twitchauth"
	"ItsBagelBot/internal/testnats"
	"ItsBagelBot/internal/valkeytest"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/bus/bustest"
	"ItsBagelBot/pkg/codec"
	"ItsBagelBot/pkg/ratelimit"
	pkg_valkey "ItsBagelBot/pkg/valkey"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/valkey-io/valkey-go"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

const (
	validatePath = "/oauth2/validate"
	chatPath     = "/helix/chat/messages"
	scopeLocale  = "fr"
	scopeSentLog = "Twitch scope notice sent"
	scopeFailLog = "Twitch scope notice send failed after claiming"
)

type scopeNoticeCase struct {
	name       string
	state      string
	nextState  string
	banned     bool
	scopes     []string
	expiry     *time.Time
	validation scriptedResponse
	chatReply  scriptedResponse
	limiter    ratelimit.Manager
	goLives    [][]string
	wantLoads  int
	wantClaims int
	wantSent   int
	wantFailed int
}

func TestHandleStreamEventScopeNotice(t *testing.T) {
	full := twitchauth.Required()
	gap := full[1:]
	chat := []string{chatPath}
	validate := []string{validatePath}
	expired := time.Now().Add(-time.Hour)
	tests := []scopeNoticeCase{
		{name: "stored gap posts once across go-lives", scopes: gap, goLives: [][]string{chat, chat}, wantLoads: 2, wantClaims: 2, wantSent: 1},
		{name: "full scopes need no notice", scopes: full, wantLoads: 1},
		{name: "known empty scopes are a gap", scopes: []string{}, goLives: [][]string{chat}, wantLoads: 1, wantClaims: 1, wantSent: 1},
		{name: "blocked channel keeps its notice", state: lifecycle.Blocked, scopes: gap},
		{name: "dormant channel keeps its notice", state: lifecycle.Dormant, scopes: gap},
		{name: "bot-banned channel keeps its notice", banned: true, scopes: gap},
		{name: "gate closes before claim", scopes: gap, nextState: lifecycle.Blocked, wantLoads: 1},
		{name: "unknown scopes with expired token skip validation", expiry: &expired, wantLoads: 1},
		{
			name: "unknown scopes validate with a gap", validation: validationReply(t, gap),
			goLives: [][]string{{validatePath, chatPath}}, wantLoads: 1, wantClaims: 1, wantSent: 1,
		},
		{
			name: "unknown scopes validate with full scopes", validation: validationReply(t, full),
			goLives: [][]string{validate}, wantLoads: 1,
		},
		{
			name: "validate reports scopes as unknown", validation: validationReply(t, nil),
			goLives: [][]string{validate}, wantLoads: 1,
		},
		{
			name: "validate fails", validation: scriptedResponse{status: http.StatusUnauthorized},
			goLives: [][]string{validate}, wantLoads: 1,
		},
		{
			name: "validation respects the Twitch budget", wantLoads: 1,
			limiter: &scriptedLimiter{denied: map[string]bool{"ratelimit:helix:system": true, helixAppKey: true}},
		},
		{
			name: "dropped chat keeps the claim", scopes: gap,
			chatReply: scriptedResponse{status: http.StatusOK, body: `{"data":[{"is_sent":false}]}`},
			goLives:   [][]string{chat, chat}, wantLoads: 2, wantClaims: 2, wantFailed: 1,
		},
		{
			name: "Twitch failure keeps the claim", scopes: gap,
			chatReply: scriptedResponse{status: http.StatusServiceUnavailable},
			goLives:   [][]string{chat, chat}, wantLoads: 2, wantClaims: 2, wantFailed: 1,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) { tc.run(t) })
	}
}

func (tc scopeNoticeCase) run(t *testing.T) {
	t.Helper()
	server := valkeytest.New(t)
	server.Handle("SET", scopeNoticeLock)
	probe := &scopeNoticeProbe{t: t, validation: tc.validation, chat: tc.chatReply}
	probe.claimExists = func(context.Context) bool {
		var exists bool
		server.With(func(s *valkeytest.Server) { exists = s.Alive(scopeNoticeKey()) })
		return exists
	}
	w, logs := tc.worker(t, probe, server.Client())

	goLives := tc.goLives
	if len(goLives) == 0 {
		goLives = [][]string{nil}
	}
	for _, want := range goLives {
		require.NoError(t, w.HandleStreamEvent(scopeGoLive(testBroadcaster)))
		awaitScopeNotice(t, w)
		assert.Equal(t, want, probe.paths())
	}
	assert.EqualValues(t, tc.wantLoads, probe.loads.Load(), "token loads")
	assert.Equal(t, tc.wantClaims, scopeNoticeClaims(server), "claim attempts")

	assert.Equal(t, tc.wantSent, logs.FilterLevelExact(zap.InfoLevel).FilterMessage(scopeSentLog).Len())
	assert.Equal(t, tc.wantFailed, logs.FilterLevelExact(zap.WarnLevel).FilterMessage(scopeFailLog).Len())
}

func TestHandleStreamEventScopeNoticeAcrossWorkers(t *testing.T) {
	client := valkeytest.Spawn(t)
	probe := &scopeNoticeProbe{t: t}
	probe.claimExists = func(ctx context.Context) bool {
		count, err := pkg_valkey.CountExisting(ctx, client, []string{scopeNoticeKey()})
		return assert.NoError(t, err) && count == 1
	}
	tc := scopeNoticeCase{scopes: twitchauth.Required()[1:]}
	first, _ := tc.worker(t, probe, client)
	second, _ := tc.worker(t, probe, client)

	require.NoError(t, first.HandleStreamEvent(scopeGoLive(testBroadcaster)))
	require.NoError(t, second.HandleStreamEvent(scopeGoLive(testBroadcaster)))
	awaitScopeNotice(t, first)
	awaitScopeNotice(t, second)

	assert.EqualValues(t, 2, probe.loads.Load())
	assert.Equal(t, []string{chatPath}, probe.paths())
}

func TestHandleStreamEventScopeNoticeDoesNotBlockOtherGoLives(t *testing.T) {
	server := valkeytest.New(t)
	probe := &scopeNoticeProbe{t: t, claimExists: func(context.Context) bool { return false }}
	w, _ := (scopeNoticeCase{}).worker(t, probe, server.Client())
	release := make(chan struct{})
	loaded := make(chan string, 2)
	w.loadBroadcasterToken = func(ctx context.Context, id string) (twitch.StoredToken, error) {
		loaded <- id
		if id != testBroadcaster {
			return twitch.StoredToken{}, nil
		}
		select {
		case <-release:
			return twitch.StoredToken{}, nil
		case <-ctx.Done():
			return twitch.StoredToken{}, ctx.Err()
		}
	}
	t.Cleanup(func() {
		close(release)
		awaitScopeNotice(t, w)
	})

	dispatched := make(chan [2]error, 1)
	const nextBroadcaster = "9876"
	go func() {
		first := w.HandleStreamEvent(scopeGoLive(testBroadcaster))
		next := w.HandleStreamEvent(scopeGoLive(nextBroadcaster))
		dispatched <- [2]error{first, next}
	}()
	select {
	case result := <-dispatched:
		require.Equal(t, [2]error{}, result)
	case <-time.After(2 * time.Second):
		t.Fatal("go-live handlers waited for the scope notice token read")
	}
	require.Eventually(t, func() bool { return len(loaded) == 2 }, 2*time.Second, time.Millisecond)
	assert.ElementsMatch(t, []string{testBroadcaster, nextBroadcaster}, []string{<-loaded, <-loaded})
}

func TestHandleStreamEventScopeNoticeTokenReadFailure(t *testing.T) {
	server := valkeytest.New(t)
	server.Handle("SET", scopeNoticeLock)
	probe := &scopeNoticeProbe{t: t, claimExists: func(context.Context) bool { return false }}
	w, logs := (scopeNoticeCase{}).worker(t, probe, server.Client())
	w.loadBroadcasterToken = unavailableScopeToken(t)
	checked := attachScopeNoticeModVerifier(t, w)

	require.NoError(t, w.HandleStreamEvent(scopeGoLive(testBroadcaster)))
	awaitScopeNotice(t, w)
	select {
	case <-checked:
	case <-time.After(time.Second):
		t.Fatal("go-live did not schedule a moderator check")
	}
	assert.Empty(t, probe.paths())
	assert.Zero(t, scopeNoticeClaims(server))
	assert.Equal(t, 1, logs.FilterMessage("Twitch scope notice could not check scopes").Len())
}

func unavailableScopeToken(t *testing.T) func(context.Context, string) (twitch.StoredToken, error) {
	t.Helper()
	nc := testnats.Connect(t)
	bustest.Respond(t, nc, "rpc.tokens.get", `{"error":"token store unavailable","code":"unavailable"}`)
	mustFlush(t, nc)
	return func(ctx context.Context, id string) (twitch.StoredToken, error) {
		_, err := tokenstore.New(nc, "rpc.tokens", id).Load(ctx)
		return twitch.StoredToken{}, err
	}
}

func attachScopeNoticeModVerifier(t *testing.T, w *Worker) <-chan bool {
	t.Helper()
	registry := &fakeModRegistry{saved: make(chan bool, 1)}
	moderator := &fakeModeratorClient{started: make(chan struct{}), release: make(chan struct{})}
	close(moderator.release)
	verifier := newModVerifier(registry, moderator, testBot, "pod", zap.NewNop())
	w.SetModVerifier(verifier)
	t.Cleanup(verifier.Close)
	return registry.saved
}

func (tc scopeNoticeCase) worker(t *testing.T, probe *scopeNoticeProbe, client valkey.Client) (*Worker, *observer.ObservedLogs) {
	t.Helper()
	core, logs := observer.New(zap.InfoLevel)
	registry := channels.New(client)
	t.Cleanup(registry.Close)
	registry.Prime(manage.Channel{BroadcasterID: testBroadcaster, Enabled: true, IsMod: true})
	runtime := &fakeRuntime{snap: tc.snapshot()}
	worker := New(Config{
		Runtime: runtime, Registry: registry, Log: zap.New(core), Lane: LaneSystem,
		Limiter: tc.budget(), Twitch: newTwitch(probe, staticBroadcaster), BotID: testBot,
		LoadBroadcasterToken: tc.loadToken(probe, runtime),
	})
	nc := testnats.Connect(t)
	respondState(t, nc, scopeLocale, rpc.CodeOK)
	mustFlush(t, nc)
	worker.SetReauthNotifier(newTestReauthNotifier(t, nc))
	return worker, logs
}

func (tc scopeNoticeCase) snapshot() channelruntime.Snapshot {
	snapshot := servingSnapshot()
	if tc.state != "" {
		snapshot.State.RuntimeState = tc.state
	}
	if tc.banned {
		snapshot.State.ChatRestriction = lifecycle.RestrictionBanned
	}
	return snapshot
}

func (tc scopeNoticeCase) budget() ratelimit.Manager {
	if tc.limiter == nil {
		return allowAll{}
	}
	return tc.limiter
}

func (tc scopeNoticeCase) loadToken(probe *scopeNoticeProbe, runtime *fakeRuntime) func(context.Context, string) (twitch.StoredToken, error) {
	return func(_ context.Context, id string) (twitch.StoredToken, error) {
		assert.Equal(probe.t, testBroadcaster, id)
		probe.loads.Add(1)
		if tc.nextState != "" {
			runtime.snap.State.RuntimeState = tc.nextState
		}
		return twitch.StoredToken{AccessToken: "stored-access", Scopes: tc.scopes, AccessTokenExpiresAt: tc.expiry}, nil
	}
}

func scopeGoLive(broadcasterID string) *bus.Message {
	return bus.NewMessage("stream-event", []byte(`{"type":"stream.online","event":{"broadcaster_user_id":"`+broadcasterID+`"}}`))
}

func awaitScopeNotice(t *testing.T, w *Worker) {
	t.Helper()
	require.Eventually(t, func() bool { return len(w.scopeNotice) == 0 }, 5*time.Second, time.Millisecond)
}

func scopeNoticeKey() string {
	return "reauthScope:" + testBroadcaster + ":" + twitchauth.SetVersion
}

type scopeNoticeProbe struct {
	t           *testing.T
	validation  scriptedResponse
	chat        scriptedResponse
	claimExists func(context.Context) bool
	mu          sync.Mutex
	calls       []string
	loads       atomic.Int32
}

func (p *scopeNoticeProbe) paths() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return slices.Clone(p.calls)
}

func (p *scopeNoticeProbe) RoundTrip(req *http.Request) (*http.Response, error) {
	p.mu.Lock()
	p.calls = append(p.calls, req.URL.Path)
	p.mu.Unlock()
	switch req.URL.Path {
	case validatePath:
		assert.Equal(p.t, []string{http.MethodGet, "id.twitch.tv", "OAuth stored-access"},
			[]string{req.Method, req.URL.Host, req.Header.Get("Authorization")})
		return scopeHTTPReply(p.validation), nil
	case chatPath:
		p.checkChat(req)
		return scopeHTTPReply(p.chat), nil
	}
	return &http.Response{StatusCode: http.StatusNoContent, Body: http.NoBody}, nil
}

func scopeHTTPReply(reply scriptedResponse) *http.Response {
	if reply.status == 0 {
		reply = scriptedResponse{status: http.StatusOK, body: `{"data":[{"is_sent":true}]}`}
	}
	return &http.Response{StatusCode: reply.status, Body: io.NopCloser(strings.NewReader(reply.body))}
}

func (p *scopeNoticeProbe) checkChat(req *http.Request) {
	assert.True(p.t, p.claimExists(req.Context()), "claim must already exist before the chat send")
	want := map[string]string{
		"broadcaster_id": testBroadcaster, "sender_id": testBot, "message": i18n.T(scopeLocale, i18n.KeyGrantScopeChat),
	}
	got := map[string]string{}
	assert.NoError(p.t, codec.NewDecoder(req.Body).Decode(&got))
	assert.Equal(p.t, http.MethodPost, req.Method)
	assert.Equal(p.t, want, got)
}

func scopeNoticeLock(s *valkeytest.Server, args valkeytest.Args) valkeytest.Reply {
	if !slices.Equal(args[1:], []string{"1", "NX", "PX", "15552000000"}) {
		return valkeytest.Err("expected a 180-day NX lock")
	}
	if s.Alive(args.Key()) {
		return valkeytest.Nil()
	}
	s.Strings[args.Key()] = args[1]
	s.Expires[args.Key()] = s.Now().Add(180 * 24 * time.Hour)
	return valkeytest.Simple("OK")
}

func scopeNoticeClaims(s *valkeytest.Server) int {
	count := 0
	for _, op := range s.Ops() {
		if op.Cmd == "SET" && op.Args.Key() == scopeNoticeKey() {
			count++
		}
	}
	return count
}

func validationReply(t *testing.T, scopes []string) scriptedResponse {
	t.Helper()
	reply, err := codec.Marshal(map[string]any{"user_id": testBroadcaster, "scopes": scopes})
	require.NoError(t, err)
	return scriptedResponse{status: http.StatusOK, body: string(reply)}
}
