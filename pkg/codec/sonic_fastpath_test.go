// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary and unlicensed. See LICENSE.md.

package codec

import (
	"testing"

	"github.com/bytedance/sonic"
)

// This is the one place outside codec.go that names the backend, and it does so
// on purpose: the guard has to ask Sonic directly which of its two
// implementations got compiled in.

// TestSonicFastPathIsLive enforces the Go version ceiling documented on the
// package. Past its validated Go release Sonic silently replaces itself with an
// encoding/json wrapper, so a toolchain bump would keep every build green and
// every test passing while quietly returning the fleet to the standard library
// encoder. Nothing else would notice. Fail here instead, at the one spot that
// can tell the difference.
func TestSonicFastPathIsLive(t *testing.T) {
	if sonic.APIKind != sonic.UseSonicJSON {
		t.Fatalf("sonic compiled its encoding/json fallback (APIKind=%d, want UseSonicJSON=%d): "+
			"the Go toolchain has moved past sonic's validated ceiling. Pin Go back, or bump sonic "+
			"to a release that supports this toolchain and update the ceiling noted in codec.go.",
			sonic.APIKind, sonic.UseSonicJSON)
	}
}
