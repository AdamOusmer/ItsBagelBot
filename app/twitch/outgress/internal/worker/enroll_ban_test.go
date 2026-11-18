// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package worker

import (
	"cmp"
	"context"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"ItsBagelBot/app/twitch/outgress/internal/twitch"
	"ItsBagelBot/internal/channelruntime"
	"ItsBagelBot/internal/domain/i18n"
	"ItsBagelBot/internal/domain/lifecycle"
	notificationsrpc "ItsBagelBot/internal/domain/rpc/notifications"
	usersrpc "ItsBagelBot/internal/domain/rpc/users"
	"ItsBagelBot/internal/testnats"
	"ItsBagelBot/internal/valkeytest"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/codec"

	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

const missingScopeBody = `{"error":"Unauthorized","status":401,"message":"Missing scope: moderation:read or moderator:manage:banned_users"}`

const missingAuthorizationBody =`{"error":"Forbidden","status":403,"message":"subscription missing proper authorization"}`

type forbiddenEnroll struct {
	forbidType string
	body       string
	banStatus  int
	banList    string
	onBanCheck func()
}

func (f forbiddenEnroll) transport(t *testing.T) subscriptionTransport {
	return func(r *http.Request) (*http.Response, error) {
		if strings.HasPrefix(r.URL.Path, "/helix/moderation/banned") {
			f.onBanCheck()
			return subscriptionResponse(f.banStatus, f.banList), nil
		}
		if r.Method == http.MethodGet {
			return subscriptionResponse(http.StatusOK, `{"data":[]}`), nil
		}
		body, _ := io.ReadAll(r.Body)
		var spec twitch.SubSpec
		require.NoError(t, codec.Unmarshal(body, &spec))
		if f.forbids(spec) {
			return subscriptionResponse(http.StatusForbidden, f.body), nil
		}
		return subscriptionResponse(http.StatusAccepted, `{}`), nil
	}
}

func (f forbiddenEnroll) forbids(spec twitch.SubSpec) bool {
	if f.forbidType == "" {
		return spec.BotChat()
	}
	return spec.Type == f.forbidType
}

type statefulRuntime struct {
	mu   sync.Mutex
	snap channelruntime.Snapshot
}

func (r *statefulRuntime) Get(context.Context, uint64) (channelruntime.Snapshot, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.snap, nil
}

func (r *statefulRuntime) Apply(_ context.Context, _ uint64, reply usersrpc.LifecycleReply) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.snap.State = reply.State
	return nil
}

type statefulAuthority struct {
	mu          sync.Mutex
	incarnation int64
	state       lifecycle.State
	refuseBan   bool
	actions     []string
}

func (a *statefulAuthority) recreate() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.incarnation++
}

func (a *statefulAuthority) handle(_ context.Context, r usersrpc.LifecycleRequest) usersrpc.LifecycleReply {
	a.mu.Lock()
	defer a.mu.Unlock()
	reply := usersrpc.LifecycleReply{AccountCreatedAt: a.incarnation, State: a.state}
	if r.AccountCreatedAt != 0 && r.AccountCreatedAt != a.incarnation {
		return reply
	}
	reply.Applied = a.apply(r)
	reply.State = a.state
	return reply
}

func (a *statefulAuthority) apply(r usersrpc.LifecycleRequest) bool {
	switch r.Action {
	case "enroll_retry":
		return a.backOff(r)
	case "scope_missing":
		a.state.IncidentID, a.state.IncidentReason = "incident-1", lifecycle.ReasonScopeMissing
		a.actions = append(a.actions, r.Action)
		return true
	case "notice_claim":
		return !a.state.IncidentAnnounced
	case "notice_ack":
		a.state.IncidentAnnounced = true
		return true
	}
	restriction, moderation := map[string]string{"ban": lifecycle.RestrictionBanned, "timeout": lifecycle.RestrictionTimeout}[r.Action]
	if !moderation {
		return r.Action == "grant_restored"
	}
	if a.refuseBan {
		return false
	}
	a.state.ChatRestriction = restriction
	a.state.AdmissionGeneration++
	a.actions = append(a.actions, r.Action)
	return true
}

func (a *statefulAuthority) backOff(r usersrpc.LifecycleRequest) bool {
	if r.AdmissionGeneration != a.state.AdmissionGeneration {
		return false
	}
	a.state.RuntimeState = lifecycle.Recovering
	a.state.AdmissionGeneration++
	a.actions = append(a.actions, r.Action)
	return true
}

func (a *statefulAuthority) serve(t *testing.T) *nats.Conn {
	t.Helper()
	nc := testnats.Connect(t)
	require.NoError(t, bus.Serve(bus.RPCWiring{NC: nc, Log: zap.NewNop(), Queue: "users"}, usersrpc.LifecycleSubject, a.handle))
	require.NoError(t, nc.Flush())
	return nc
}

func (a *statefulAuthority) applied() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return strings.Join(a.actions, ",")
}

type enrollBanOutcome struct {
	state      string
	moderation string
	admitted   bool
}

func TestChatEnrollForbiddenRecordsOnlyAConfirmedRestriction(t *testing.T) {
	permanent := `{"data":[{"user_id":"` + testBot + `","expires_at":""}]}`
	timedOut := `{"data":[{"user_id":"` + testBot + `","expires_at":"` + time.Now().Add(time.Hour).UTC().Format(time.RFC3339) + `"}]}`
	stillFailing := enrollBanOutcome{state: subStateFailing, admitted: true}
	backedOff := enrollBanOutcome{state: subStateFailing, moderation: "enroll_retry"}
	cases := []struct {
		name      string
		enroll    forbiddenEnroll
		refuseBan bool
		recreate  bool
		want      enrollBanOutcome
	}{
		{name: "permanent ban stops admission", enroll: forbiddenEnroll{banList: permanent},
			want: enrollBanOutcome{state: subStateBanned, moderation: "ban"}},
		{name: "timeout takes the expiring timeout path", enroll: forbiddenEnroll{banList: timedOut},
			want: enrollBanOutcome{state: subStateBanned, moderation: "timeout"}},
		{name: "bot absent from the ban list backs off into recovery", enroll: forbiddenEnroll{banList: `{"data":[]}`}, want: backedOff},
		{name: "ban list refused for a missing scope backs off and asks for the scope", enroll: forbiddenEnroll{banStatus: http.StatusUnauthorized, banList: missingScopeBody},
			want: enrollBanOutcome{state: subStateFailing, moderation: "enroll_retry,scope_missing"}},
		{name: "ban list refused without a scope message only backs off", enroll: forbiddenEnroll{banStatus: http.StatusUnauthorized, banList: `{"message":"Invalid OAuth token"}`}, want: backedOff},
		{name: "authorization 403 on a non-chat type stays failing", enroll: forbiddenEnroll{forbidType: "channel.follow", banList: permanent}, want: stillFailing},
		{name: "generic 403 on chat stays failing", enroll: forbiddenEnroll{body: `{"error":"Forbidden","status":403,"message":"Forbidden"}`, banList: permanent}, want: stillFailing},
		{name: "ban list outage backs off into recovery", enroll: forbiddenEnroll{banStatus: http.StatusServiceUnavailable, banList: `{"message":"outage"}`}, want: backedOff},
		{name: "authority refusing the ban backs off into recovery", enroll: forbiddenEnroll{banList: permanent}, refuseBan: true, want: backedOff},
		{name: "account recreated during the ban check is not restricted", enroll: forbiddenEnroll{banList: permanent}, recreate: true, want: stillFailing},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			serving := servingSnapshot()
			authority := &statefulAuthority{incarnation: serving.AccountCreatedAt, state: serving.State, refuseBan: tc.refuseBan}
			enroll := tc.enroll
			enroll.body = cmp.Or(enroll.body, missingAuthorizationBody)
			enroll.banStatus = cmp.Or(enroll.banStatus, http.StatusOK)
			enroll.onBanCheck = func() {
				if tc.recreate {
					authority.recreate()
				}
			}
			w := verifyWorker(t, enroll.transport(t), valkeytest.Spawn(t))
			w.runtime = &statefulRuntime{snap: serving}
			w.lifecycleNC = authority.serve(t)

			require.NoError(t, w.HandleAuthzGranted(grantEvent(t)))

			state, _ := storedSubState(t, w)
			got := enrollBanOutcome{state: state, moderation: authority.applied(), admitted: w.BackgroundAllowed(t.Context(), testBroadcaster)}
			assert.Equal(t, tc.want, got)
		})
	}
}

func chatBanRevocation(t *testing.T) *bus.Message {
	t.Helper()
	body, err := codec.Marshal(authzSubRevoked{
		BroadcasterID: testBroadcaster, Type: twitch.ChatMessageType, Status: statusChatUserBanned,
		SubscriptionID: "legacy-chat-sub", UserID: testBot, EventID: "revoke-1", ObservedAt: time.Now().UTC(),
	})
	require.NoError(t, err)
	return &bus.Message{Payload: body}
}

func TestChatBanRevocationOnAChannelWithoutRecordedOwnership(t *testing.T) {
	permanent := `{"data":[{"user_id":"` + testBot + `","expires_at":""}]}`
	cases := []struct {
		name      string
		chatSubID string
		banStatus int
		banList   string
		want      string
	}{
		{name: "readable ban is recorded", banStatus: http.StatusOK, banList: permanent, want: "ban"},
		{name: "ban list unreadable for a missing scope backs off and asks for the scope", banStatus: http.StatusUnauthorized, banList: missingScopeBody, want: "enroll_retry,scope_missing"},
		{name: "revocation of a subscription the channel no longer owns is ignored", chatSubID: "current-chat-sub", banStatus: http.StatusOK, banList: permanent},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			serving := servingSnapshot()
			serving.State.ChatSubscriptionID = tc.chatSubID
			authority := &statefulAuthority{incarnation: serving.AccountCreatedAt, state: serving.State}
			enroll := forbiddenEnroll{body: missingAuthorizationBody, banStatus: tc.banStatus, banList: tc.banList, onBanCheck: func() {}}
			w := verifyWorker(t, enroll.transport(t), valkeytest.Spawn(t))
			w.runtime = &statefulRuntime{snap: serving}
			w.lifecycleNC = authority.serve(t)

			require.NoError(t, w.HandleAuthzSubRevoked(chatBanRevocation(t)))

			assert.Equal(t, tc.want, authority.applied())
		})
	}
}

func TestMissingScopeAsksTheOwnerToApproveTheNewPermission(t *testing.T) {
	serving := servingSnapshot()
	authority := &statefulAuthority{incarnation: serving.AccountCreatedAt, state: serving.State}
	enroll := forbiddenEnroll{body: missingAuthorizationBody, banStatus: http.StatusUnauthorized, banList: missingScopeBody, onBanCheck: func() {}}
	w := verifyWorker(t, enroll.transport(t), valkeytest.Spawn(t))
	w.runtime = &statefulRuntime{snap: serving}
	nc := authority.serve(t)
	w.lifecycleNC = nc
	w.reauth = NewReauthNotifier(nc, ReauthConfig{StateSubject: "test.users.state"}, zap.NewNop())
	require.NoError(t, bus.Serve(bus.RPCWiring{NC: nc, Log: zap.NewNop(), Queue: "users"}, "test.users.state",
		func(context.Context, usersrpc.StateGetRequest) usersrpc.StateGetReply { return usersrpc.StateGetReply{Locale: "en"} }))
	sent := make(chan notificationsrpc.ConnectionRequest, 1)
	require.NoError(t, bus.Serve(bus.RPCWiring{NC: nc, Log: zap.NewNop(), Queue: "notifications"}, notificationsrpc.ConnectionSubject+".send",
		func(_ context.Context, r notificationsrpc.ConnectionRequest) notificationsrpc.ConnectionReply {
			sent <- r
			return notificationsrpc.ConnectionReply{Outcome: "accepted"}
		}))
	require.NoError(t, nc.Flush())

	require.NoError(t, w.HandleAuthzGranted(grantEvent(t)))

	notice := <-sent
	assert.Equal(t, i18n.T("en", i18n.KeyGrantScopeTitle), notice.Title)
	assert.Contains(t, notice.Body, i18n.DashboardURL)
}
