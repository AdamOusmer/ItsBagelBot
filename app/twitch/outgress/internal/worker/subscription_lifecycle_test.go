package worker

import (
	"ItsBagelBot/app/twitch/outgress/internal/twitch"
	"ItsBagelBot/pkg/codec"
	"context"
	"github.com/stretchr/testify/require"
	"io"
	"net/http"
	"strings"
	"testing"
)

type subscriptionTransport func(*http.Request) (*http.Response, error)

func (f subscriptionTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func subscriptionResponse(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}
}
func TestRecoveryScopeFailureDoesNotAbortNormalChatEnrollment(t *testing.T) {
	var chat bool
	transport := subscriptionTransport(func(r *http.Request) (*http.Response, error) {
		body, _ := io.ReadAll(r.Body)
		var spec twitch.SubSpec
		require.NoError(t, codec.Unmarshal(body, &spec))
		if controlSubscription(spec.Type) {
			return subscriptionResponse(403, `{"message":"subscription missing proper authorization"}`), nil
		}
		if spec.Type == twitch.ChatMessageType {
			chat = true
		}
		return subscriptionResponse(202, `{}`), nil
	})
	w := pipelineWorker(t, transport)
	require.NoError(t, w.createAllEventSubs(context.Background(), enrollment{broadcasterID: testBroadcaster, conduitID: "conduit"}))
	require.True(t, chat, "ordinary authorized chat must enroll despite missing channel:moderate")
}
func TestStaleDisableCannotDiscoverReplacementSubscriptions(t *testing.T) {
	calls := 0
	w := pipelineWorker(t, subscriptionTransport(func(*http.Request) (*http.Response, error) { calls++; return subscriptionResponse(202, `{}`), nil }))
	runtime := &fakeRuntime{servingSnapshot()}
	w.runtime = runtime
	err := w.disableEventSubs(context.Background(), enrollment{broadcasterID: testBroadcaster, incarnation: 122, generation: 2})
	require.ErrorIs(t, err, errLifecycleStopped)
	require.Zero(t, calls)
}
func TestDisableRechecksGenerationBetweenExactDeletions(t *testing.T) {
	runtime := &fakeRuntime{servingSnapshot()}
	deletes := 0
	w := pipelineWorker(t, subscriptionTransport(func(r *http.Request) (*http.Response, error) {
		if r.Method == http.MethodGet {
			return subscriptionResponse(200, `{"data":[{"id":"old-a","transport":{"conduit_id":"conduit"},"condition":{"broadcaster_user_id":"`+testBroadcaster+`"}},{"id":"old-b","transport":{"conduit_id":"conduit"},"condition":{"broadcaster_user_id":"`+testBroadcaster+`"}}]}`), nil
		}
		deletes++
		require.Equal(t, "old-a", r.URL.Query().Get("id"))
		runtime.snap.State.AdmissionGeneration++
		return subscriptionResponse(204, ``), nil
	}))
	w.runtime = runtime
	err := w.disableEventSubs(context.Background(), enrollment{broadcasterID: testBroadcaster, conduitID: "conduit", incarnation: 123, generation: 2})
	require.ErrorIs(t, err, errLifecycleStopped)
	require.Equal(t, 1, deletes)
}
func TestBotOwnershipExcludesIndependentStreamsAndOtherChatUsers(t *testing.T) {
	w := &Worker{botID: "bot"}
	sub := twitch.EventSubEntry{Type: "channel.subscribe"}
	sub.Condition.BroadcasterUserID = "42"
	require.True(t, w.botOwnedSubscription(sub))
	sub.Type = "stream.online"
	require.False(t, w.botOwnedSubscription(sub))
	sub.Type = twitch.ChatMessageType
	sub.Condition.UserID = "other"
	require.False(t, w.botOwnedSubscription(sub))
	sub.Condition.UserID = "bot"
	require.True(t, w.botOwnedSubscription(sub))
}
func TestBannedGrantRepairCreatesOnlyRecoveryListeners(t *testing.T) {
	var kinds []string
	w := pipelineWorker(t, subscriptionTransport(func(r *http.Request) (*http.Response, error) {
		body, _ := io.ReadAll(r.Body)
		var spec twitch.SubSpec
		require.NoError(t, codec.Unmarshal(body, &spec))
		kinds = append(kinds, spec.Type)
		return subscriptionResponse(202, `{}`), nil
	}))
	runtime := &fakeRuntime{servingSnapshot()}
	runtime.snap.State.RuntimeState = "blocked"
	runtime.snap.State.ChatRestriction = "banned"
	w.runtime = runtime
	e := enrollment{broadcasterID: testBroadcaster, conduitID: "conduit", incarnation: 123, generation: 2}
	require.False(t, w.enrollmentAllowed(context.Background(), e))
	require.NoError(t, w.createControlListeners(context.Background(), e))
	require.Equal(t, []string{"channel.ban", "channel.unban"}, kinds)
}
func TestTransientRecoveryListenerFailureIsReturnedForDurableRetry(t *testing.T) {
	w := pipelineWorker(t, subscriptionTransport(func(*http.Request) (*http.Response, error) {
		return subscriptionResponse(503, `{"message":"outage"}`), nil
	}))
	require.Error(t, w.createControlListeners(context.Background(), enrollment{broadcasterID: testBroadcaster, conduitID: "conduit"}))
}
