// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package worker

import (
	"context"
	"io"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"ItsBagelBot/app/twitch/outgress/internal/channels"
	"ItsBagelBot/app/twitch/outgress/internal/twitch"
	"ItsBagelBot/internal/domain/i18n"
	"ItsBagelBot/internal/domain/lifecycle"
	"ItsBagelBot/internal/domain/rpc"
	"ItsBagelBot/internal/domain/rpc/manage"
	"ItsBagelBot/internal/domain/twitchauth"
	"ItsBagelBot/internal/testnats"
	"ItsBagelBot/internal/valkeytest"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/codec"
	"ItsBagelBot/pkg/ratelimit"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

const (
	validatePath = "/oauth2/validate"
	chatPath     = "/helix/chat/messages"
	scopeLocale  = "fr"
)

type scopeNoticeCase struct {
	name       string
	state      string
	banned     bool
	scopes     []string
	validation scriptedResponse
	limiter    ratelimit.Manager
	goLives    [][]string
	wantLoads  int
	wantClaims int
}

func TestHandleStreamEventScopeNotice(t *testing.T) {
	full := twitchauth.Required()
	gap := full[1:]
	chat := []string{chatPath}
	validate := []string{validatePath}
	tests := []scopeNoticeCase{
		{name: "stored gap posts once across go-lives", scopes: gap, goLives: [][]string{chat, chat}, wantLoads: 2, wantClaims: 2},
		{name: "full scopes need no notice", scopes: full, wantLoads: 1},
		{name: "known empty scopes are a gap", scopes: []string{}, goLives: [][]string{chat}, wantLoads: 1, wantClaims: 1},
		{name: "blocked channel keeps its notice", state: lifecycle.Blocked, scopes: gap},
		{name: "dormant channel keeps its notice", state: lifecycle.Dormant, scopes: gap},
		{name: "bot-banned channel keeps its notice", banned: true, scopes: gap},
		{
			name: "unknown scopes validate with a gap", validation: validationReply(t, gap),
			goLives: [][]string{{validatePath, chatPath}}, wantLoads: 1, wantClaims: 1,
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
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) { tc.run(t) })
	}
}

func (tc scopeNoticeCase) run(t *testing.T) {
	t.Helper()
	valkey := valkeytest.New(t)
	valkey.Handle("SET", scopeNoticeLock)
	key := "reauthScope:" + testBroadcaster + ":" + twitchauth.SetVersion
	probe := &scopeNoticeProbe{t: t, validation: tc.validation, valkey: valkey, key: key}
	worker := tc.worker(t, probe)

	goLives := tc.goLives
	if len(goLives) == 0 {
		goLives = [][]string{nil}
	}
	for _, want := range goLives {
		msg := bus.NewMessage("stream-event", []byte(`{"type":"stream.online","event":{"broadcaster_user_id":"`+testBroadcaster+`"}}`))
		require.NoError(t, worker.HandleStreamEvent(msg))
		assert.Equal(t, want, probe.calls)
	}
	assert.Equal(t, tc.wantLoads, probe.loads)
	assert.Equal(t, tc.wantClaims, scopeNoticeClaims(valkey, key))
}

func (tc scopeNoticeCase) worker(t *testing.T, probe *scopeNoticeProbe) *Worker {
	t.Helper()
	snapshot := servingSnapshot()
	if tc.state != "" {
		snapshot.State.RuntimeState = tc.state
	}
	if tc.banned {
		snapshot.State.ChatRestriction = lifecycle.RestrictionBanned
	}
	registry := channels.New(probe.valkey.Client())
	t.Cleanup(registry.Close)
	registry.Prime(manage.Channel{BroadcasterID: testBroadcaster, Enabled: true, IsMod: true})
	limiter := tc.limiter
	if limiter == nil {
		limiter = allowAll{}
	}
	worker := New(Config{
		Runtime: &fakeRuntime{snap: snapshot}, Registry: registry, Log: zap.NewNop(), Lane: LaneSystem,
		Limiter: limiter, Twitch: newTwitch(probe, staticBroadcaster), BotID: testBot,
		LoadBroadcasterToken: func(_ context.Context, id string) (twitch.StoredToken, error) {
			assert.Equal(t, testBroadcaster, id)
			probe.loads++
			return twitch.StoredToken{AccessToken: "stored-access", Scopes: tc.scopes}, nil
		},
	})
	nc := testnats.Connect(t)
	respondState(t, nc, scopeLocale, rpc.CodeOK)
	mustFlush(t, nc)
	worker.SetReauthNotifier(newTestReauthNotifier(t, nc))
	return worker
}

type scopeNoticeProbe struct {
	t          *testing.T
	validation scriptedResponse
	valkey     *valkeytest.Server
	key        string
	calls      []string
	loads      int
}

func (p *scopeNoticeProbe) RoundTrip(req *http.Request) (*http.Response, error) {
	p.calls = append(p.calls, req.URL.Path)
	switch req.URL.Path {
	case validatePath:
		assert.Equal(p.t, []string{http.MethodGet, "id.twitch.tv", "OAuth stored-access"},
			[]string{req.Method, req.URL.Host, req.Header.Get("Authorization")})
		return &http.Response{StatusCode: p.validation.status, Body: io.NopCloser(strings.NewReader(p.validation.body))}, nil
	case chatPath:
		p.checkChat(req)
	}
	return &http.Response{StatusCode: http.StatusNoContent, Body: http.NoBody}, nil
}

func (p *scopeNoticeProbe) checkChat(req *http.Request) {
	p.valkey.With(func(s *valkeytest.Server) {
		assert.True(p.t, s.Alive(p.key), "claim must already exist before the chat send")
	})
	want := map[string]string{
		"broadcaster_id": testBroadcaster, "sender_id": testBot, "message": i18n.T(scopeLocale, i18n.KeyGrantScopeChat),
	}
	got := map[string]string{}
	require.NoError(p.t, codec.NewDecoder(req.Body).Decode(&got))
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

func scopeNoticeClaims(s *valkeytest.Server, key string) int {
	count := 0
	for _, op := range s.Ops() {
		if op.Cmd == "SET" && op.Args.Key() == key {
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
