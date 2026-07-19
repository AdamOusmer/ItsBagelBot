package worker

import (
	"context"
	"errors"
	"testing"

	"ItsBagelBot/app/outgress/internal/twitch"
	"ItsBagelBot/internal/domain/rpc/manage"

	"go.uber.org/zap"
)

// fakeGrants records writes so a test can assert on transitions without Valkey.
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

func testWorker(g grantRegistry) *Worker {
	return &Worker{log: zap.NewNop(), grants: g}
}

// deadGrantErr is the incident's error, wrapped the way client.do wraps it.
func deadGrantErr() error {
	return &twitch.TokenError{Status: 400, Body: `{"status":400,"message":"Invalid refresh token"}`}
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
		// The fleet-safety cases. execute.go carries app chat sends and bot
		// moderation under real broadcaster ids, and the app, bot and broadcaster
		// grants all fail through the same postToken. Without the identity gate,
		// one client-credentials outage would mark every broadcaster whose
		// traffic happened to pass during it, and chat being the highest-volume
		// type, that is the entire enrolled fleet within seconds.
		{"app identity never marks", twitch.IdentityApp, registered, true, deadGrantErr(), nil},
		{"bot identity never marks", twitch.IdentityBot, registered, true, deadGrantErr(), nil},
		{"auto identity never marks", twitch.IdentityAuto, registered, true, deadGrantErr(), nil},

		// A dead grant, seen under the broadcaster's own identity.
		{
			"broadcaster identity marks dead",
			twitch.IdentityBroadcaster, registered, true, deadGrantErr(),
			[]manage.GrantState{manage.GrantDead},
		},

		// Transient failures must never tell a streamer their connection expired.
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

		// Success is the inverse write, and the self-heal for a false positive.
		{
			"success clears a dead marker",
			twitch.IdentityBroadcaster, alreadyDead, true, nil,
			[]manage.GrantState{manage.GrantUnknown},
		},
		{"success on a healthy channel is a no-op", twitch.IdentityBroadcaster, registered, true, nil, nil},

		// Transition-only, so a channel failing every few seconds does not
		// hammer Valkey or re-log forever.
		{"already dead does not rewrite", twitch.IdentityBroadcaster, alreadyDead, true, deadGrantErr(), nil},

		// Never conjure a registry entry for a channel the bot does not serve.
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

// TestNoteGrantHealthUnreadableRegistry keeps a Valkey blip from being read as
// a healthy channel and clearing a real marker.
func TestNoteGrantHealthUnreadableRegistry(t *testing.T) {
	g := &fakeGrants{getErr: errors.New("valkey down")}
	w := testWorker(g)

	w.noteGrantHealth(context.Background(), twitch.IdentityBroadcaster, "1", nil)

	assertWrites(t, g.writes, nil)
}

func TestNoteGrantHealthNoRegistryIsNoop(t *testing.T) {
	w := testWorker(nil)
	// Must not panic.
	w.noteGrantHealth(context.Background(), twitch.IdentityBroadcaster, "1", deadGrantErr())
}

func TestClearGrantDead(t *testing.T) {
	tests := []struct {
		name    string
		channel manage.Channel
		want    []manage.GrantState
	}{
		// sub_state "ok" is exactly the state a grant-dead channel sits in, and
		// exactly the state the re-enroll gate rejects. This is why the clear is
		// placed above that gate.
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

// TestLiveNotice pins which notice a go-live picks, including the tie-break.
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
			got, ok := liveNotice(tc.channel)
			if ok != tc.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tc.wantOK)
			}
			if got != tc.want {
				t.Errorf("notice = %+v, want %+v", got, tc.want)
			}
		})
	}
}

// TestNoticeRequestPrefixesDiffer guards the dedupe. The notifications unique
// index on request_id has no time component and the send verb skips cache
// invalidation on the dedupe path, so a shared prefix would make the second
// notice of the day silently vanish.
func TestNoticeRequestPrefixesDiffer(t *testing.T) {
	if noticeRevoked.request == noticeGrantDead.request {
		t.Fatalf("both notices share request prefix %q, so one would be deduped away",
			noticeRevoked.request)
	}
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
