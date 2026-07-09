package worker

import (
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	"ItsBagelBot/internal/domain/outgress"
)

func TestDrainResponseEnablesHTTP11ConnectionReuse(t *testing.T) {
	var connections atomic.Int64
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, strings.Repeat("x", 1024))
	}))
	server.EnableHTTP2 = false
	server.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		if state == http.StateNew {
			connections.Add(1)
		}
	}
	server.Start()
	defer server.Close()

	client := server.Client()
	for range 2 {
		res, err := client.Get(server.URL)
		if err != nil {
			t.Fatal(err)
		}
		drainResponse(res)
	}
	if got := connections.Load(); got != 1 {
		t.Fatalf("connections = %d, want 1", got)
	}
}

type countingReadCloser struct {
	remaining int
	read      int
}

func (r *countingReadCloser) Read(p []byte) (int, error) {
	if r.remaining == 0 {
		return 0, io.EOF
	}
	n := min(len(p), r.remaining)
	r.remaining -= n
	r.read += n
	return n, nil
}

func (*countingReadCloser) Close() error { return nil }

func TestDrainResponseIsBounded(t *testing.T) {
	body := &countingReadCloser{remaining: maxResponseDrain * 2}
	drainResponse(&http.Response{Body: body})
	if body.read != maxResponseDrain+1 {
		t.Fatalf("drained %d bytes, want %d", body.read, maxResponseDrain+1)
	}
}

func TestCloudBotChatActionsUseAppToken(t *testing.T) {
	for _, typ := range []string{outgress.TypeChat, outgress.TypeAnnounce, outgress.TypeShoutout} {
		route := typeRoutes[typ]
		if route.as != outgress.AsApp {
			t.Fatalf("%s route identity = %q, want %q", typ, route.as, outgress.AsApp)
		}
	}
}

func TestGeneralHelixRequestsUseTokenSpecificBuckets(t *testing.T) {
	tests := []struct {
		name       string
		message    outgress.Message
		sharedKey  string
		sharedPref string
		scope      string
		value      string
	}{
		{
			name:      "app token",
			message:   outgress.Message{As: outgress.AsApp, Endpoint: "/helix/users"},
			sharedKey: "ratelimit:helix:app",
		},
		{
			name:      "bot user token",
			message:   outgress.Message{As: outgress.AsBot, Endpoint: "/helix/moderation/bans"},
			sharedKey: "ratelimit:helix:user:bot",
		},
		{
			name:      "auto-routed bot user token",
			message:   outgress.Message{Endpoint: "/helix/moderation/channels?first=100"},
			sharedKey: "ratelimit:helix:user:bot",
		},
		{
			name:       "broadcaster user token",
			message:    outgress.Message{As: outgress.AsBroadcaster, BroadcasterID: "123", Endpoint: "/helix/clips"},
			sharedPref: "ratelimit:helix:user:", scope: "helix:user", value: "123",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, shared := generalHelixRequests(tc.message)
			got := [4]string{shared.Key, shared.DynamicPrefix, shared.Bucket.Scope, shared.Bucket.Value}
			want := [4]string{tc.sharedKey, tc.sharedPref, tc.scope, tc.value}
			if got != want {
				t.Fatalf("shared request (key/prefix/scope/value) = %v, want %v", got, want)
			}
		})
	}
}

type withFieldCase struct {
	name  string
	body  []byte
	field string
	value string
	want  string
}

func runWithFieldCases(t *testing.T, cases []withFieldCase) {
	t.Helper()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := string(withField(tc.body, tc.field, tc.value))
			if got != tc.want {
				t.Fatalf("withField(%q, %q, %q) = %q, want %q",
					tc.body, tc.field, tc.value, got, tc.want)
			}
		})
	}
}

func TestWithFieldObjectBodies(t *testing.T) {
	runWithFieldCases(t, []withFieldCase{
		{
			name:  "empty object gets bare field",
			body:  []byte(`{}`),
			field: "sender_id",
			value: "x",
			want:  `{"sender_id":"x"}`,
		},
		{
			name:  "non-empty object appends with comma",
			body:  []byte(`{"a":"b"}`),
			field: "sender_id",
			value: "x",
			want:  `{"a":"b","sender_id":"x"}`,
		},
		{
			name:  "field already present is idempotent",
			body:  []byte(`{"sender_id":"already"}`),
			field: "sender_id",
			value: "x",
			want:  `{"sender_id":"already"}`,
		},
		{
			name:  "whitespace before closing brace",
			body:  []byte("{\"a\":\"b\"  \n\t}"),
			field: "color",
			value: "primary",
			want:  "{\"a\":\"b\"  \n\t,\"color\":\"primary\"}",
		},
		{
			name:  "whitespace-only empty object stays bare",
			body:  []byte("{   }"),
			field: "color",
			value: "primary",
			want:  `{   "color":"primary"}`,
		},
		{
			name:  "color merges into message body",
			body:  []byte(`{"message":"hello"}`),
			field: "color",
			value: "blue",
			want:  `{"message":"hello","color":"blue"}`,
		},
	})
}

func TestWithFieldNonObjectBodies(t *testing.T) {
	runWithFieldCases(t, []withFieldCase{
		{
			name:  "nil body produces minimal object",
			body:  nil,
			field: "sender_id",
			value: "x",
			want:  `{"sender_id":"x"}`,
		},
		{
			name:  "top-level array body returned unchanged",
			body:  []byte(`["a","b"]`),
			field: "sender_id",
			value: "x",
			want:  `["a","b"]`,
		},
		{
			name:  "bare scalar body returned unchanged",
			body:  []byte(`"hi"`),
			field: "sender_id",
			value: "x",
			want:  `"hi"`,
		},
		{
			name:  "empty body synthesizes object",
			body:  []byte(``),
			field: "sender_id",
			value: "x",
			want:  `{"sender_id":"x"}`,
		},
		{
			name:  "whitespace-only body synthesizes object",
			body:  []byte("  \n\t "),
			field: "sender_id",
			value: "x",
			want:  `{"sender_id":"x"}`,
		},
	})
}

func TestWithSenderIDWrapsWithField(t *testing.T) {
	got := string(withSenderID([]byte(`{"message":"hi"}`), "12345"))
	want := `{"message":"hi","sender_id":"12345"}`
	if got != want {
		t.Fatalf("withSenderID = %q, want %q", got, want)
	}
}

// TestAnnounceEndpoint pins the query-param construction processAnnounce uses:
// broadcaster_id + moderator_id ride the query string, not the body, and are
// URL-escaped. This mirrors the endpoint assembly in processAnnounce without a
// network round-trip.
func TestAnnounceEndpoint(t *testing.T) {
	broadcasterID := "44322889"
	mod := "987654"
	ep := "/helix/chat/announcements?broadcaster_id=" +
		url.QueryEscape(broadcasterID) + "&moderator_id=" + url.QueryEscape(mod)
	want := "/helix/chat/announcements?broadcaster_id=44322889&moderator_id=987654"
	if ep != want {
		t.Fatalf("announce endpoint = %q, want %q", ep, want)
	}

	body := string(withField([]byte(`{"message":"gm"}`), "color", "primary"))
	wantBody := `{"message":"gm","color":"primary"}`
	if body != wantBody {
		t.Fatalf("announce body = %q, want %q", body, wantBody)
	}
}

// TestShoutoutEndpoint pins the query-param construction processShoutout uses:
// from_broadcaster_id + to_broadcaster_id + moderator_id ride the query string
// (no body) and are URL-escaped. Mirrors the endpoint assembly without a network
// round-trip.
func TestShoutoutEndpoint(t *testing.T) {
	ep := shoutoutEndpoint("44322889", "12826", "987654")
	want := "/helix/chat/shoutouts?from_broadcaster_id=44322889&to_broadcaster_id=12826&moderator_id=987654"
	if ep != want {
		t.Fatalf("shoutout endpoint = %q, want %q", ep, want)
	}

	// Ids needing escaping are escaped (defense-in-depth; real ids are numeric).
	got := shoutoutEndpoint("a b", "c&d", "e?f")
	wantEsc := "/helix/chat/shoutouts?from_broadcaster_id=a+b&to_broadcaster_id=c%26d&moderator_id=e%3Ff"
	if got != wantEsc {
		t.Fatalf("shoutout endpoint (escaped) = %q, want %q", got, wantEsc)
	}
}

// assertRoute pins one type's Helix routing: method, endpoint, and token
// identity.
func assertRoute(t *testing.T, typ string, want helixRoute) {
	t.Helper()
	route, ok := typeRoutes[typ]
	if !ok {
		t.Fatalf("%s has no type route", typ)
	}
	if route != want {
		t.Fatalf("%s route = %+v, want %+v", typ, route, want)
	}
}

// TestShieldModeRoute pins the Shield Mode routing: a PUT to the moderation
// shield_mode endpoint under the bot's moderator token, so the automod's
// mass-raid escalation lands as a moderator action, not an app call.
func TestShieldModeRoute(t *testing.T) {
	assertRoute(t, outgress.TypeShieldMode,
		helixRoute{http.MethodPut, "/helix/moderation/shield_mode", outgress.AsBot})
}

// TestShieldModeEndpoint mirrors the query-param assembly processShieldMode uses:
// broadcaster_id + moderator_id ride the query string, URL-escaped.
func TestShieldModeEndpoint(t *testing.T) {
	ep := "/helix/moderation/shield_mode?broadcaster_id=" +
		url.QueryEscape("44322889") + "&moderator_id=" + url.QueryEscape("987654")
	want := "/helix/moderation/shield_mode?broadcaster_id=44322889&moderator_id=987654"
	if ep != want {
		t.Fatalf("shield_mode endpoint = %q, want %q", ep, want)
	}
}

// TestDeleteAndWarnRoutes pins the moderator-action routing for the automod's
// delete (Delete Chat Messages) and warn (Warn Chat User) intents.
func TestDeleteAndWarnRoutes(t *testing.T) {
	assertRoute(t, outgress.TypeDelete,
		helixRoute{http.MethodDelete, "/helix/moderation/chat", outgress.AsBot})
	assertRoute(t, outgress.TypeWarn,
		helixRoute{http.MethodPost, "/helix/moderation/warnings", outgress.AsBot})
}

// TestDeleteEndpoint pins the query assembly processDelete uses: all three ids
// on the query string, URL-escaped, no body.
func TestDeleteEndpoint(t *testing.T) {
	got := deleteEndpoint("44322889", "987654", "abc-123")
	want := "/helix/moderation/chat?broadcaster_id=44322889&moderator_id=987654&message_id=abc-123"
	if got != want {
		t.Fatalf("delete endpoint = %q, want %q", got, want)
	}
	if esc := deleteEndpoint("a b", "c&d", "e?f"); esc !=
		"/helix/moderation/chat?broadcaster_id=a+b&moderator_id=c%26d&message_id=e%3Ff" {
		t.Fatalf("delete endpoint (escaped) = %q", esc)
	}
}
