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

// TestMarkerIgnoresNonBroadcasterIdentity is the fleet-safety test. execute.go
// carries app chat sends and bot moderation under real broadcaster ids, and the
// app/bot/broadcaster grants all fail through the same postToken. Without the
// identity gate, one client-credentials outage would mark the whole enrolled
// fleet dead and nag every streamer in chat at their next go-live.
func TestMarkerIgnoresNonBroadcasterIdentity(t *testing.T) {
	for _, id := range []twitch.Identity{twitch.IdentityApp, twitch.IdentityBot, twitch.IdentityAuto} {
		g := &fakeGrants{channel: manage.Channel{BroadcasterID: "1"}, found: true}
		w := testWorker(g)

		w.noteGrantHealth(context.Background(), id, "1", deadGrantErr())

		if len(g.writes) != 0 {
			t.Errorf("identity %v: wrote %v, want no write", id, g.writes)
		}
	}
}

func TestMarkerWritesDeadForBroadcasterIdentity(t *testing.T) {
	g := &fakeGrants{channel: manage.Channel{BroadcasterID: "1"}, found: true}
	w := testWorker(g)

	w.noteGrantHealth(context.Background(), twitch.IdentityBroadcaster, "1", deadGrantErr())

	if len(g.writes) != 1 || g.writes[0] != manage.GrantDead {
		t.Fatalf("writes = %v, want [dead]", g.writes)
	}
}

// TestMarkerIgnoresTransientFailures keeps a Twitch outage or a network blip
// from telling streamers their connection expired.
func TestMarkerIgnoresTransientFailures(t *testing.T) {
	transient := []error{
		&twitch.TokenError{Status: 500, Body: "internal"},
		&twitch.TokenError{Status: 429, Body: "slow down"},
		errors.New("dial tcp: i/o timeout"),
	}

	for _, err := range transient {
		g := &fakeGrants{channel: manage.Channel{BroadcasterID: "1"}, found: true}
		w := testWorker(g)

		w.noteGrantHealth(context.Background(), twitch.IdentityBroadcaster, "1", err)

		if len(g.writes) != 0 {
			t.Errorf("err %v: wrote %v, want no write", err, g.writes)
		}
	}
}

// TestMarkerClearsOnSuccess is one of the two escape hatches: a false positive
// heals itself on the next successful broadcaster call.
func TestMarkerClearsOnSuccess(t *testing.T) {
	g := &fakeGrants{
		channel: manage.Channel{BroadcasterID: "1", GrantState: manage.GrantDead},
		found:   true,
	}
	w := testWorker(g)

	w.noteGrantHealth(context.Background(), twitch.IdentityBroadcaster, "1", nil)

	if len(g.writes) != 1 || g.writes[0] != manage.GrantUnknown {
		t.Fatalf("writes = %v, want [unknown]", g.writes)
	}
}

// TestMarkerWritesOnlyOnTransition keeps a channel that fails every few seconds
// from hammering Valkey and re-logging forever.
func TestMarkerWritesOnlyOnTransition(t *testing.T) {
	g := &fakeGrants{
		channel: manage.Channel{BroadcasterID: "1", GrantState: manage.GrantDead},
		found:   true,
	}
	w := testWorker(g)

	for range 5 {
		w.noteGrantHealth(context.Background(), twitch.IdentityBroadcaster, "1", deadGrantErr())
	}

	if len(g.writes) != 0 {
		t.Fatalf("writes = %v, want none (already dead)", g.writes)
	}
}

// TestMarkerSkipsUnregisteredChannel stops the marker conjuring a registry entry
// for a channel the bot does not serve.
func TestMarkerSkipsUnregisteredChannel(t *testing.T) {
	g := &fakeGrants{found: false}
	w := testWorker(g)

	w.noteGrantHealth(context.Background(), twitch.IdentityBroadcaster, "1", deadGrantErr())

	if len(g.writes) != 0 {
		t.Fatalf("writes = %v, want none (channel not registered)", g.writes)
	}
}

func TestMarkerNoRegistryIsNoop(t *testing.T) {
	w := testWorker(nil)
	// Must not panic.
	w.noteGrantHealth(context.Background(), twitch.IdentityBroadcaster, "1", deadGrantErr())
}

// TestClearGrantDeadOnReconsent covers the second escape hatch. It must run for
// a channel whose sub_state is "ok", which is exactly the state a grant-dead
// channel sits in and the state the re-enroll gate rejects.
func TestClearGrantDeadOnReconsent(t *testing.T) {
	g := &fakeGrants{found: true}
	w := testWorker(g)
	ch := manage.Channel{BroadcasterID: "1", SubState: "ok", GrantState: manage.GrantDead}

	w.clearGrantDead(context.Background(), "1", ch)

	if len(g.writes) != 1 || g.writes[0] != manage.GrantUnknown {
		t.Fatalf("writes = %v, want [unknown]", g.writes)
	}
}

func TestClearGrantDeadSkipsHealthyChannel(t *testing.T) {
	g := &fakeGrants{found: true}
	w := testWorker(g)

	w.clearGrantDead(context.Background(), "1", manage.Channel{BroadcasterID: "1"})

	if len(g.writes) != 0 {
		t.Fatalf("writes = %v, want none", g.writes)
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
