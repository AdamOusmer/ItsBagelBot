// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package worker

import (
	"context"
	"net/http"
	"testing"

	"ItsBagelBot/internal/domain/outgress"

	"github.com/stretchr/testify/require"
)

func TestUnbanResolutionAndBrandedContentIdentities(t *testing.T) {
	runActionCases(t, []actionCase{
		{
			name: "approve uses the bot grant and escapes the request and explanation",
			msg:  testMessage{Type: "unban_resolve", Payload: `{"request_id":"req &9","status":"approved","resolution":" welcome back! "}`},
			want: []recordedRequest{call("PATCH", "/helix/moderation/unban_requests?broadcaster_id="+testBroadcaster+"&"+mod+"&resolution_text=welcome+back%21&status=approved&unban_request_id=req+%269", botAuth, "")},
		},
		{
			name: "deny omits an empty explanation",
			msg:  testMessage{Type: "unban_resolve", Payload: `{"request_id":"req-9","status":"denied"}`},
			want: []recordedRequest{call("PATCH", "/helix/moderation/unban_requests?broadcaster_id="+testBroadcaster+"&"+mod+"&status=denied&unban_request_id=req-9", botAuth, "")},
		},
		{name: "missing request id is dropped", msg: testMessage{Type: "unban_resolve", Payload: `{"status":"approved"}`}},
		{name: "unknown resolution is dropped", msg: testMessage{Type: "unban_resolve", Payload: `{"request_id":"9","status":"allow"}`}},
		{
			name: "branded content uses the broadcaster grant",
			msg:  testMessage{Type: "branded_content", Payload: `{"enabled":true}`},
			want: []recordedRequest{call("PATCH", "/helix/channels?broadcaster_id="+testBroadcaster, broadcasterAuth, `{"is_branded_content":true}`)},
		},
		{
			name: "branded content can be disabled",
			msg:  testMessage{Type: "branded_content", Payload: `{"enabled":false}`},
			want: []recordedRequest{call("PATCH", "/helix/channels?broadcaster_id="+testBroadcaster, broadcasterAuth, `{"is_branded_content":false}`)},
		},
	})
}

func TestTypedActionRechecksLifecycleAfterLookup(t *testing.T) {
	for _, typ := range []string{"poll_end", "vip_add"} {
		t.Run(typ, func(t *testing.T) {
			runtime := &fakeRuntime{servingSnapshot()}
			calls := 0
			w := pipelineWorker(t, subscriptionTransport(func(*http.Request) (*http.Response, error) {
				calls++
				runtime.snap.State.AdmissionGeneration++
				return subscriptionResponse(200, `{"data":[{"id":"9","status":"ACTIVE","login":"bob"}]}`), nil
			}))
			w.runtime = runtime
			msg := &outgress.Message{Type: typ, BroadcasterID: testBroadcaster, AccountCreatedAt: 123, AdmissionGeneration: 2, To: "bob", Payload: []byte(`{}`)}
			require.ErrorIs(t, w.processDecoded(context.Background(), msg, "stream-lookup"), errLifecycleStopped)
			require.Equal(t, 1, calls, "a stale lookup must never be followed by the mutation")
		})
	}
}

func TestSnoozeRetriesGenericRateLimits(t *testing.T) {
	rt := &scriptedTransport{responses: []scriptedResponse{{status: 429, body: `{"message":"Too Many Requests"}`}}}
	err := (testMessage{Type: "ad_snooze", Payload: `{}`}).send(pipelineWorker(t, rt))
	require.Error(t, err)
	require.Equal(t, 1, rt.callCount())
}

func TestPollEndReservesBothHelixCalls(t *testing.T) {
	rt := &scriptedTransport{responses: []scriptedResponse{{status: 200, body: `{"data":[{"id":"p1","status":"ACTIVE"}]}`}}}
	w := pipelineWorker(t, rt)
	limiter := &scriptedLimiter{}
	w.limiter = limiter
	require.NoError(t, (testMessage{Type: "poll_end", Payload: `{}`}).send(w))
	require.Len(t, limiter.calls, 2, "reading the latest poll and ending it each consume quota")
}
