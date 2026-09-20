// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package gateway

import "testing"

// A resume dials READY's resume_gateway_url, which Discord hands back bare. It
// has to carry the query of the first connection (v=10, encoding), or the
// resume lands on a different gateway version and is answered with op 9 --
// which is what happened to every resume before this.
func TestDialURLForCarriesTheGatewayQueryOntoTheResumeURL(t *testing.T) {
	st := &resumeState{}
	st.ready("sess-1", "wss://gateway-us-east1-b.discord.gg")
	got := dialURLFor(gatewayURL, st)
	want := "wss://gateway-us-east1-b.discord.gg/?v=10&encoding=json"
	if got != want {
		t.Fatalf("dial url = %q, want %q", got, want)
	}
}

func TestWithGatewayQuery(t *testing.T) {
	cases := []struct{ name, resume, gateway, want string }{
		{"bare resume url takes the gateway query", "wss://r.discord.gg", gatewayURL, "wss://r.discord.gg/?v=10&encoding=json"},
		{"resume url with its own query is kept", "wss://r.discord.gg/?v=9", gatewayURL, "wss://r.discord.gg/?v=9"},
		{"gateway url without a query adds nothing", "ws://resume", "ws://x", "ws://resume"},
		{"unparseable resume url passes through", "://nope", gatewayURL, "://nope"},
	}
	for _, tc := range cases {
		if got := withGatewayQuery(tc.resume, tc.gateway); got != tc.want {
			t.Fatalf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}
}
