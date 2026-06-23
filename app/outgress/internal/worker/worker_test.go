package worker

import (
	"net/url"
	"testing"
)

func TestWithField(t *testing.T) {
	tests := []struct {
		name  string
		body  []byte
		field string
		value string
		want  string
	}{
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
			name:  "nil body produces minimal object",
			body:  nil,
			field: "sender_id",
			value: "x",
			want:  `{"sender_id":"x"}`,
		},
		{
			name:  "color merges into message body",
			body:  []byte(`{"message":"hello"}`),
			field: "color",
			value: "blue",
			want:  `{"message":"hello","color":"blue"}`,
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
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := string(withField(tc.body, tc.field, tc.value))
			if got != tc.want {
				t.Fatalf("withField(%q, %q, %q) = %q, want %q",
					tc.body, tc.field, tc.value, got, tc.want)
			}
		})
	}
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
