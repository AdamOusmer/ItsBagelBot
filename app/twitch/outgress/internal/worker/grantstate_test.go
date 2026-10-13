// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package worker

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"ItsBagelBot/app/twitch/outgress/internal/twitch"
	"ItsBagelBot/internal/domain/rpc/manage"

	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

type fakeGrants struct {
	channel manage.Channel
	found   bool
	getErr  error
	writes  []manage.GrantState
}

func (f *fakeGrants) Get(context.Context, string) (manage.Channel, bool, error) {
	return f.channel, f.found, f.getErr
}

func (f *fakeGrants) SetGrantState(_ context.Context, _ string, state manage.GrantState) error {
	f.writes = append(f.writes, state)
	f.channel.GrantState = state
	return nil
}

func deadTokenSource(string) *twitch.Source {
	return twitch.NewStoredUserTokenSource(twitch.ClientCredentials{}, "", twitch.StoredTokenIO{
		Load: func(context.Context) twitch.StoredLoad { return twitch.StoredLoad{} },
	}, twitch.MintLease{})
}

func TestGrantStateFollowsTheBroadcasterTokenHealth(t *testing.T) {
	registered := manage.Channel{BroadcasterID: testBroadcaster}
	dead := manage.Channel{BroadcasterID: testBroadcaster, GrantState: manage.GrantDead}
	tests := []struct {
		name      string
		as        string
		deadToken bool
		channel   manage.Channel
		found     bool
		getErr    error
		want      []manage.GrantState
	}{
		{"marks a registered channel dead when its grant is dead", "broadcaster", true, registered, true, nil, []manage.GrantState{manage.GrantDead}},
		{"does not rewrite a channel that is already dead", "broadcaster", true, dead, true, nil, nil},
		{"skips an unregistered channel", "broadcaster", true, manage.Channel{}, false, nil, nil},
		{"leaves state alone when the registry is unreadable", "broadcaster", true, registered, true, errors.New("valkey down"), nil},
		{"clears a dead marker after a successful call", "broadcaster", false, dead, true, nil, []manage.GrantState{manage.GrantUnknown}},
		{"writes nothing after a successful call on a healthy channel", "broadcaster", false, registered, true, nil, nil},
		{"never marks the app identity", "app", true, registered, true, nil, nil},
		{"never marks the bot identity", "bot", true, registered, true, nil, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			grants := &fakeGrants{channel: tt.channel, found: tt.found, getErr: tt.getErr}
			var tw *twitch.Client
			if tt.deadToken {
				tw = twitch.NewClient("client", deadTokenSource(""), deadTokenSource(""), twitch.NewBroadcasterTokens(deadTokenSource))
			} else {
				tw = newTwitch(&scriptedTransport{}, staticBroadcaster)
			}
			w := pipelineWorker(t, &scriptedTransport{}, withTwitch(tw))
			w.grants = grants

			_ = testMessage{Type: "api", Method: http.MethodGet, As: tt.as, Endpoint: "/helix/users"}.send(w)

			assert.Equal(t, tt.want, grants.writes)
		})
	}
}

func testWorker(g grantRegistry) *Worker {
	return &Worker{log: zap.NewNop(), grants: g}
}

func deadGrantErr() error {
	return &twitch.TokenError{Status: 400, Body: `{"status":400,"message":"Invalid refresh token"}`}
}

func storedTokenErr(load twitch.StoredLoad) error {
	src := twitch.NewStoredUserTokenSource(twitch.ClientCredentials{}, "", twitch.StoredTokenIO{
		Load:    func(context.Context) twitch.StoredLoad { return load },
		Persist: func(context.Context, string, string, time.Time) error { return nil },
	}, twitch.MintLease{})
	_, err := src.Token(context.Background())
	return err
}

func assertWrites(t *testing.T, got, want []manage.GrantState) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("writes = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("writes = %v, want %v", got, want)
		}
	}
}

func TestNoteGrantHealth(t *testing.T) {
	registered := manage.Channel{BroadcasterID: "1"}
	alreadyDead := manage.Channel{BroadcasterID: "1", GrantState: manage.GrantDead}

	tests := []struct {
		name     string
		identity twitch.Identity
		channel  manage.Channel
		found    bool
		err      error
		want     []manage.GrantState
	}{
		{"app identity never marks", twitch.IdentityApp, registered, true, deadGrantErr(), nil},
		{"bot identity never marks", twitch.IdentityBot, registered, true, deadGrantErr(), nil},
		{"auto identity never marks", twitch.IdentityAuto, registered, true, deadGrantErr(), nil},

		{
			"broadcaster identity marks dead",
			twitch.IdentityBroadcaster, registered, true, deadGrantErr(),
			[]manage.GrantState{manage.GrantDead},
		},

		{
			"500 does not mark",
			twitch.IdentityBroadcaster, registered, true,
			&twitch.TokenError{Status: 500, Body: "internal"}, nil,
		},
		{
			"429 does not mark",
			twitch.IdentityBroadcaster, registered, true,
			&twitch.TokenError{Status: 429, Body: "slow down"}, nil,
		},
		{
			"transport failure does not mark",
			twitch.IdentityBroadcaster, registered, true,
			errors.New("dial tcp: i/o timeout"), nil,
		},

		{
			"failed token load does not mark",
			twitch.IdentityBroadcaster, registered, true,
			storedTokenErr(twitch.StoredLoad{Err: errors.New("tokens get rpc: nats: timeout")}), nil,
		},
		{
			"no stored token marks dead",
			twitch.IdentityBroadcaster, registered, true,
			storedTokenErr(twitch.StoredLoad{}),
			[]manage.GrantState{manage.GrantDead},
		},

		{
			"success clears a dead marker",
			twitch.IdentityBroadcaster, alreadyDead, true, nil,
			[]manage.GrantState{manage.GrantUnknown},
		},
		{"success on a healthy channel is a no-op", twitch.IdentityBroadcaster, registered, true, nil, nil},

		{"already dead does not rewrite", twitch.IdentityBroadcaster, alreadyDead, true, deadGrantErr(), nil},

		{"unregistered channel is skipped", twitch.IdentityBroadcaster, manage.Channel{}, false, deadGrantErr(), nil},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			g := &fakeGrants{channel: tc.channel, found: tc.found}
			w := testWorker(g)

			w.noteGrantHealth(context.Background(), tc.identity, "1", tc.err)

			assertWrites(t, g.writes, tc.want)
		})
	}
}

func TestNoteGrantHealthUnreadableRegistry(t *testing.T) {
	g := &fakeGrants{getErr: errors.New("valkey down")}
	w := testWorker(g)

	w.noteGrantHealth(context.Background(), twitch.IdentityBroadcaster, "1", nil)

	assertWrites(t, g.writes, nil)
}

func TestNoteGrantHealthNoRegistryIsNoop(t *testing.T) {
	w := testWorker(nil)
	w.noteGrantHealth(context.Background(), twitch.IdentityBroadcaster, "1", deadGrantErr())
}

func TestClearGrantDead(t *testing.T) {
	tests := []struct {
		name    string
		channel manage.Channel
		want    []manage.GrantState
	}{
		{
			"dead grant on an ok channel clears",
			manage.Channel{BroadcasterID: "1", SubState: "ok", GrantState: manage.GrantDead},
			[]manage.GrantState{manage.GrantUnknown},
		},
		{"healthy channel is untouched", manage.Channel{BroadcasterID: "1"}, nil},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			g := &fakeGrants{found: true}
			w := testWorker(g)

			w.clearGrantDead(context.Background(), "1", tc.channel)

			assertWrites(t, g.writes, tc.want)
		})
	}
}

func TestLiveNotice(t *testing.T) {
	tests := []struct {
		name    string
		channel manage.Channel
		want    notice
		wantOK  bool
	}{
		{"healthy", manage.Channel{SubState: "ok"}, notice{}, false},
		{"unknown", manage.Channel{}, notice{}, false},
		{"revoked", manage.Channel{SubState: subStateRevoked}, noticeRevoked, true},
		{"grant dead", manage.Channel{SubState: "ok", GrantState: manage.GrantDead}, noticeGrantDead, true},
		{
			"both: revoked wins",
			manage.Channel{SubState: subStateRevoked, GrantState: manage.GrantDead},
			noticeRevoked,
			true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, _, ok := liveNotice(tc.channel)
			if ok != tc.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tc.wantOK)
			}
			if got != tc.want {
				t.Errorf("notice = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestLiveNoticeEpisodeMatchesTriggeringState(t *testing.T) {
	blockedAt := time.Unix(1700000000, 0)
	grantAt := time.Unix(1700000500, 0)

	tests := []struct {
		name    string
		channel manage.Channel
		want    time.Time
	}{
		{"banned uses BlockedAt", manage.Channel{SubState: subStateBanned, BlockedAt: blockedAt, GrantCheckedAt: grantAt}, blockedAt},
		{"revoked uses BlockedAt", manage.Channel{SubState: subStateRevoked, BlockedAt: blockedAt, GrantCheckedAt: grantAt}, blockedAt},
		{"grant dead uses GrantCheckedAt", manage.Channel{SubState: "ok", GrantState: manage.GrantDead, BlockedAt: blockedAt, GrantCheckedAt: grantAt}, grantAt},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, episode, ok := liveNotice(tc.channel)
			if !ok {
				t.Fatal("liveNotice() ok = false, want true")
			}
			if !episode.Equal(tc.want) {
				t.Errorf("episode = %v, want %v", episode, tc.want)
			}
		})
	}
}

func TestNoticeRequestPrefixesDiffer(t *testing.T) {
	if noticeRevoked.request == noticeGrantDead.request {
		t.Fatalf("both notices share request prefix %q, so one would be deduped away",
			noticeRevoked.request)
	}
}
