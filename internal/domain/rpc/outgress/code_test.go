// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package outgress

import (
	"testing"

	"ItsBagelBot/internal/domain/rpc"
	"ItsBagelBot/pkg/codec"
)

// TestCodeWireValues pins the byte each code puts on the wire. Half the set
// now aliases the shared vocabulary in internal/domain/rpc, and the console's
// DISCORD_CODES switches on these exact strings, so a rename here must fail
// this test before it turns a timed-out setup into a silent success.
func TestCodeWireValues(t *testing.T) {
	want := map[rpc.Code]string{
		CodeOK:                 "",
		CodeBoundElsewhere:     "bound_elsewhere",
		CodeNotBound:           "not_bound",
		CodeDiscordUnavailable: "discord_unavailable",
		CodeForbidden:          "forbidden",
		CodeRateLimited:        "rate_limited",
		CodeInvalid:            "invalid",
		CodeNotFound:           "not_found",
		CodeTimeout:            "timeout",
		CodeConflict:           "conflict",
		CodeUnknown:            "unknown",
	}
	for code, text := range want {
		if string(code) != text {
			t.Errorf("code %q serialises as %q", text, string(code))
		}
	}
	raw, err := codec.Marshal(DiscordUnbindReply{Code: CodeTimeout})
	if err != nil {
		t.Fatal(err)
	}
	if got := string(raw); got != `{"code":"timeout"}` {
		t.Fatalf("wire = %s", got)
	}
}
