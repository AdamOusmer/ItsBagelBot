// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package codec

import (
	"testing"

	"github.com/bytedance/sonic"
)

func TestSonicFastPathIsLive(t *testing.T) {
	if sonic.APIKind != sonic.UseSonicJSON {
		t.Fatalf("sonic compiled its encoding/json fallback (APIKind=%d, want UseSonicJSON=%d): "+
			"the Go toolchain has moved past sonic's validated ceiling. Pin Go back, or bump sonic "+
			"to a release that supports this toolchain and update the ceiling noted in codec.go.",
			sonic.APIKind, sonic.UseSonicJSON)
	}
}
